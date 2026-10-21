package azureblob

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"

	"github.com/formancehq/go-libs/v5/pkg/storage"
	"github.com/formancehq/go-libs/v5/pkg/storage/internal/objectio"
	"github.com/formancehq/go-libs/v5/pkg/storage/object"
)

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) Do(r *http.Request) (*http.Response, error) { return f(r) }
func azureClient(t *testing.T, transport testTransport) *azblob.Client {
	t.Helper()
	client, err := azblob.NewClientWithNoCredential("https://storage.invalid", &azblob.ClientOptions{ClientOptions: azcore.ClientOptions{Transport: transport, Retry: policy.RetryOptions{MaxRetries: -1}}})
	if err != nil {
		t.Fatal(err)
	}
	return client
}
func TestAzureConditionalRangeAndRevisionFailure(t *testing.T) {
	status := 206
	client := azureClient(t, func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("If-Match") != "\"v1\"" {
			t.Fatalf("missing condition: %v", r.Header)
		}
		if strings.Join(r.Header["x-ms-range"], "") != "bytes=0-2" {
			t.Fatalf("missing range: %v", r.Header)
		}
		h := http.Header{"Content-Length": {"3"}, "Content-Range": {"bytes 0-2/6"}, "Etag": {"\"v1\""}, "Content-Type": {"application/octet-stream"}}
		if status == 412 {
			h.Set("x-ms-error-code", "ConditionNotMet")
		}
		return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader("abc")), Request: r}, nil
	})
	bucket, err := NewBucket(client, "container", object.Limits{MaxBytes: 4})
	if err != nil {
		t.Fatal(err)
	}
	result, err := bucket.Read(t.Context(), object.ReadRequest{Key: "key", Revision: "\"v1\"", PrefixBytes: 3})
	if err != nil || result.Complete || string(result.Data) != "abc" {
		t.Fatalf("%+v %v", result, err)
	}
	status = 412
	if _, err := bucket.Read(t.Context(), object.ReadRequest{Key: "key", Revision: "\"v1\"", PrefixBytes: 3}); !errors.Is(err, object.ErrRevisionChanged) {
		t.Fatal(err)
	}
}
func TestAzureListingMetadataAndClock(t *testing.T) {
	body := `<?xml version="1.0"?><EnumerationResults ServiceEndpoint="https://storage.invalid" ContainerName="container"><Blobs><Blob><Name>item</Name><Properties><Last-Modified>Fri, 16 Oct 2026 10:00:00 GMT</Last-Modified><Etag>"v1"</Etag><Content-Length>3</Content-Length><BlobType>BlockBlob</BlobType></Properties></Blob></Blobs><NextMarker>next</NextMarker></EnumerationResults>`
	client := azureClient(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("maxresults") != "2" {
			t.Fatalf("request not bounded: %v", r.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/xml"}, "Date": {"Fri, 16 Oct 2026 10:01:00 GMT"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	bucket, err := NewBucket(client, "container", object.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	page, err := bucket.ListPage(t.Context(), object.ListRequest{Limit: 2})
	if err != nil || len(page.Objects) != 1 || page.NextCursor != "next" || page.Objects[0].Revision != "\"v1\"" || page.ServerTime.IsZero() {
		t.Fatalf("%+v %v", page, err)
	}
	if _, err := bucket.ListPage(t.Context(), object.ListRequest{Limit: 2, Cursor: "next"}); !errors.Is(err, object.ErrListingLimit) {
		t.Fatal(err)
	}
}

func TestAzureStatAndCompleteRead(t *testing.T) {
	client := azureClient(t, func(r *http.Request) (*http.Response, error) {
		headers := http.Header{"Content-Length": {"3"}, "Last-Modified": {"Fri, 16 Oct 2026 10:00:00 GMT"}, "Etag": {"\"v1\""}, "Content-Type": {"application/octet-stream"}}
		body := "abc"
		if r.Method == http.MethodHead {
			body = ""
		}
		return &http.Response{StatusCode: 200, Header: headers, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	bucket, err := NewBucket(client, "container", object.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	meta, data, err := object.ReadCurrent(t.Context(), bucket, "item", 3)
	if err != nil || meta.Revision != "\"v1\"" || string(data) != "abc" {
		t.Fatalf("%+v %v", meta, err)
	}
	if _, err := bucket.Read(t.Context(), object.ReadRequest{Key: "item", MaxBytes: 2}); !errors.Is(err, object.ErrTooLarge) {
		t.Fatal(err)
	}
}
func TestAzureMalformedListingAndCachedClock(t *testing.T) {
	client := azureClient(t, func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/xml"}}, Body: io.NopCloser(strings.NewReader(`<EnumerationResults><Blobs><Blob><Name>item</Name></Blob></Blobs></EnumerationResults>`)), Request: r}, nil
	})
	bucket, err := NewBucket(client, "container", object.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bucket.ListPage(t.Context(), object.ListRequest{Limit: 1}); !errors.Is(err, object.ErrInvalid) {
		t.Fatal(err)
	}
	if got := objectio.ServerTime(&http.Response{Header: http.Header{"Date": {"Fri, 16 Oct 2026 10:00:00 GMT"}, "Age": {"0"}}}); !got.IsZero() {
		t.Fatal("cached response treated as current clock")
	}
	for _, tc := range []struct {
		code   string
		status int
		want   error
	}{{"ConditionNotMet", 412, object.ErrRevisionChanged}} {
		cause := &azcore.ResponseError{ErrorCode: tc.code, StatusCode: tc.status}
		err := azureError(cause)
		if !errors.Is(err, tc.want) || !errors.Is(err, cause) {
			t.Fatalf("lost classification %s", tc.code)
		}
	}
}

// Authorization failures stay SDK errors: callers retain their retry/stop policy.
func TestAzureAuthorizationDoesNotInventTerminalVerdicts(t *testing.T) {
	for _, code := range []string{"AuthenticationFailed", "InvalidAuthenticationInfo", "AuthorizationPermissionMismatch"} {
		cause := &azcore.ResponseError{ErrorCode: code, StatusCode: http.StatusForbidden}
		got := azureError(cause)
		if got != cause || errors.Is(got, storage.ErrCredentials) || errors.Is(got, storage.ErrPermission) {
			t.Fatalf("changed authorization policy for %s: %v", code, got)
		}
	}
}

func TestAzureCancellationAndConstructorBounds(t *testing.T) {
	client := azureClient(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("canceled work reached transport")
		return nil, nil
	})
	bucket, err := NewBucket(client, "container", object.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := bucket.ListPage(ctx, object.ListRequest{Limit: 1}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := bucket.Stat(ctx, "key"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := bucket.Read(ctx, object.ReadRequest{Key: "key"}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := NewBucket(nil, "container", object.Limits{}); !errors.Is(err, object.ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := NewBucket(client, "", object.Limits{}); !errors.Is(err, object.ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := NewBucket(client, "container", object.Limits{MaxPages: -1}); !errors.Is(err, object.ErrInvalid) {
		t.Fatal(err)
	}
}

func TestAzureAggregateBudgetsAndContinuation(t *testing.T) {
	for _, kind := range []string{"complete", "repeated", "objects", "pages", "empty pages"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			client := azureClient(t, func(r *http.Request) (*http.Response, error) {
				calls++
				marker := fmt.Sprintf("m%d", calls)
				if kind == "repeated" {
					marker = "same"
				}
				if kind == "complete" && calls == 2 {
					marker = ""
				}
				items := `<Blob><Name>item</Name><Properties><Content-Length>1</Content-Length><Last-Modified>Fri, 16 Oct 2026 10:00:00 GMT</Last-Modified><Etag>"v1"</Etag></Properties></Blob>`
				if kind == "empty pages" {
					items = ""
				}
				body := `<EnumerationResults><Blobs>` + items + `</Blobs><NextMarker>` + marker + `</NextMarker></EnumerationResults>`
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/xml"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			bounds := object.Limits{MaxPages: 2}
			if kind == "objects" {
				bounds = object.Limits{MaxObjects: 2}
			}
			bucket, err := NewBucket(client, "container", bounds)
			if err != nil {
				t.Fatal(err)
			}
			got, err := object.ListAll(t.Context(), bucket, "", object.Limits{})
			if kind == "complete" {
				if err != nil || len(got) != 2 || calls != 2 {
					t.Fatalf("%+v %v %d", got, err, calls)
				}
				return
			}
			if !errors.Is(err, object.ErrListingLimit) || got != nil || calls != 2 {
				t.Fatalf("partial inventory: %+v %v %d", got, err, calls)
			}
		})
	}
}
