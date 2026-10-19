package s3

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/formancehq/go-libs/v5/pkg/storage/object"
)

func TestS3PageCarriesOnlyUncachedProviderClock(t *testing.T) {
	for name, headers := range map[string]http.Header{
		"provider Date": {"Date": {"Tue, 08 Sep 2026 12:00:00 GMT"}},
		"cached Date":   {"Date": {"Tue, 08 Sep 2026 12:00:00 GMT"}, "Age": {"0"}},
		"missing Date":  {},
		"invalid Date":  {"Date": {"invalid"}},
	} {
		t.Run(name, func(t *testing.T) {
			client := s3.New(s3.Options{Region: "us-east-1", BaseEndpoint: aws.String("https://storage.example"), UsePathStyle: true, Credentials: credentials.NewStaticCredentialsProvider("test-id", "test-secret", ""), HTTPClient: &http.Client{Transport: answerTransport(func(request *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: headers, Request: request, Body: io.NopCloser(strings.NewReader(`<ListBucketResult><IsTruncated>false</IsTruncated></ListBucketResult>`))}, nil
			})}})
			store, createErr := NewBucket(client, "archive", object.Limits{})
			if createErr != nil {
				t.Fatal(createErr)
			}
			page, err := store.ListPage(t.Context(), object.ListRequest{Limit: 1})
			if err != nil {
				t.Fatal(err)
			}
			if name == "provider Date" {
				if !page.ServerTime.Equal(time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)) {
					t.Fatalf("storage clock=%v", page.ServerTime)
				}
			} else if !page.ServerTime.IsZero() {
				t.Fatalf("untrusted/missing Date fell back to a client clock: %v", page.ServerTime)
			}
		})
	}
}
