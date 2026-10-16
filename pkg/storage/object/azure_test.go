package object

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
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
	bucket, err := NewAzure(client, "container", Limits{MaxBytes: 4})
	if err != nil {
		t.Fatal(err)
	}
	result, err := bucket.Read(t.Context(), ReadRequest{Key: "key", Revision: "\"v1\"", PrefixBytes: 3})
	if err != nil || result.Complete || string(result.Data) != "abc" {
		t.Fatalf("%+v %v", result, err)
	}
	status = 412
	if _, err := bucket.Read(t.Context(), ReadRequest{Key: "key", Revision: "\"v1\"", PrefixBytes: 3}); !errors.Is(err, ErrRevisionChanged) {
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
	bucket, err := NewAzure(client, "container", Limits{})
	if err != nil {
		t.Fatal(err)
	}
	page, err := bucket.ListPage(t.Context(), ListRequest{Limit: 2})
	if err != nil || len(page.Objects) != 1 || page.NextCursor != "next" || page.Objects[0].Revision != "\"v1\"" || page.ServerTime.IsZero() {
		t.Fatalf("%+v %v", page, err)
	}
	if _, err := bucket.ListPage(t.Context(), ListRequest{Limit: 2, Cursor: "next"}); !errors.Is(err, ErrListingLimit) {
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
	bucket, err := NewAzure(client, "container", Limits{})
	if err != nil {
		t.Fatal(err)
	}
	meta, data, err := ReadCurrent(t.Context(), bucket, "item", 3)
	if err != nil || meta.Revision != "\"v1\"" || string(data) != "abc" {
		t.Fatalf("%+v %v", meta, err)
	}
	if _, err := bucket.Read(t.Context(), ReadRequest{Key: "item", MaxBytes: 2}); !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
}
func TestAzureMalformedListingAndCachedClock(t *testing.T) {
	client := azureClient(t, func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/xml"}}, Body: io.NopCloser(strings.NewReader(`<EnumerationResults><Blobs><Blob><Name>item</Name></Blob></Blobs></EnumerationResults>`)), Request: r}, nil
	})
	bucket, err := NewAzure(client, "container", Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bucket.ListPage(t.Context(), ListRequest{Limit: 1}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if got := serverTime(&http.Response{Header: http.Header{"Date": {"Fri, 16 Oct 2026 10:00:00 GMT"}, "Age": {"0"}}}); !got.IsZero() {
		t.Fatal("cached response treated as current clock")
	}
	for _, tc := range []struct {
		code   string
		status int
		want   error
	}{{"AuthenticationFailed", 403, ErrCredentials}, {"AuthorizationPermissionMismatch", 403, ErrPermission}, {"ConditionNotMet", 412, ErrRevisionChanged}} {
		cause := &azcore.ResponseError{ErrorCode: tc.code, StatusCode: tc.status}
		err := azureError(cause)
		if !errors.Is(err, tc.want) || !errors.Is(err, cause) {
			t.Fatalf("lost classification %s", tc.code)
		}
	}
}

func TestAzureCancellationAndConstructorBounds(t *testing.T) {
	client := azureClient(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("canceled work reached transport")
		return nil, nil
	})
	bucket, err := NewAzure(client, "container", Limits{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := bucket.ListPage(ctx, ListRequest{Limit: 1}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := bucket.Stat(ctx, "key"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := bucket.Read(ctx, ReadRequest{Key: "key"}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := NewAzure(nil, "container", Limits{}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := NewAzure(client, "", Limits{}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := NewAzure(client, "container", Limits{MaxPages: -1}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}
