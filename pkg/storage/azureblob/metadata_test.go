package azureblob

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/formancehq/go-libs/v5/pkg/storage/object"
	s3store "github.com/formancehq/go-libs/v5/pkg/storage/s3"
)

// These responses pass through real SDK deserialization, not hand-built outputs.
func TestCloudMetadataRejectsNegativeSizes(t *testing.T) {
	for _, size := range []int64{-1, 0, 2} {
		for _, operation := range []string{"s3-stat", "azure-stat", "azure-list"} {
			t.Run(fmt.Sprintf("%s/%d", operation, size), func(t *testing.T) {
				transport := testTransport(func(r *http.Request) (*http.Response, error) {
					headers := http.Header{"Content-Length": {fmt.Sprint(size)}, "Etag": {"\"v1\""}, "Last-Modified": {"Fri, 16 Oct 2026 10:00:00 GMT"}}
					body := ""
					if operation == "azure-list" {
						headers.Del("Content-Length")
						headers.Set("Content-Type", "application/xml")
						body = fmt.Sprintf(`<EnumerationResults><Blobs><Blob><Name>item</Name><Properties><Content-Length>%d</Content-Length><Last-Modified>Fri, 16 Oct 2026 10:00:00 GMT</Last-Modified><Etag>"v1"</Etag></Properties></Blob></Blobs><NextMarker/></EnumerationResults>`, size)
					}
					return &http.Response{StatusCode: 200, Header: headers, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
				})
				var meta object.Metadata
				var err error
				if operation == "s3-stat" {
					client := awss3.New(awss3.Options{Region: "test", Credentials: credentials.NewStaticCredentialsProvider("synthetic", "synthetic", ""), HTTPClient: transport, Retryer: aws.NopRetryer{}})
					bucket, createErr := s3store.NewBucket(client, "bucket", object.Limits{})
					if createErr != nil {
						t.Fatal(createErr)
					}
					meta, err = bucket.Stat(t.Context(), "item")
				} else {
					bucket, createErr := NewBucket(azureClient(t, transport), "container", object.Limits{})
					if createErr != nil {
						t.Fatal(createErr)
					}
					if operation == "azure-stat" {
						meta, err = bucket.Stat(t.Context(), "item")
					} else {
						page, listErr := bucket.ListPage(t.Context(), object.ListRequest{Limit: 1})
						err = listErr
						if size < 0 && len(page.Objects) != 0 {
							t.Fatal("invalid listing returned partial metadata")
						}
						if len(page.Objects) == 1 {
							meta = page.Objects[0]
						}
					}
				}
				if size < 0 {
					if !errors.Is(err, object.ErrInvalid) || meta != (object.Metadata{}) {
						t.Fatalf("negative metadata accepted: %+v %v", meta, err)
					}
					return
				}
				if err != nil || meta.Key != "item" || meta.Size != size {
					t.Fatalf("valid metadata rejected: %+v %v", meta, err)
				}
			})
		}
	}
}

func TestAzureStatPreservesAbsentRevisionForConsumerPolicy(t *testing.T) {
	client := azureClient(t, func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Length": {"3"}, "Last-Modified": {"Fri, 16 Oct 2026 10:00:00 GMT"}}, Body: http.NoBody, Request: r}, nil
	})
	bucket, err := NewBucket(client, "container", object.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	meta, err := bucket.Stat(t.Context(), "item")
	if err != nil || meta.Key != "item" || meta.Size != 3 || meta.Revision != "" {
		t.Fatalf("%+v %v", meta, err)
	}
}
