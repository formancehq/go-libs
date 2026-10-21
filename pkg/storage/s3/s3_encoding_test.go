package s3

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/formancehq/go-libs/v5/pkg/storage/object"
)

func TestS3SDKListingKeysRoundTripLosslessly(t *testing.T) {
	keys := []string{"item\x01+%", "literal%2B+", "unicodé/雪"}
	token := "opaque+%2B/token"
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("list-type") == "2" {
			if r.URL.Query().Get("encoding-type") != "url" {
				t.Error("listing did not request URL encoding")
			}
			cursor := r.URL.Query().Get("continuation-token")
			index := 0
			if cursor != "" {
				if cursor != token {
					t.Errorf("cursor changed: %q", cursor)
				}
				index = 1
			}
			encoded := strings.ReplaceAll(url.PathEscape(keys[index]), "+", "%2B")
			w.Header().Set("Content-Type", "application/xml")
			_, _ = fmt.Fprintf(w, `<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><EncodingType>url</EncodingType><IsTruncated>%t</IsTruncated><NextContinuationToken>%s</NextContinuationToken><Contents><Key>%s</Key><LastModified>2026-01-02T03:04:05Z</LastModified><Size>3</Size></Contents>`, index == 0, token, encoded)
			if index == 1 {
				_, _ = fmt.Fprintf(w, `<Contents><Key>%s</Key><LastModified>2026-01-02T03:04:05Z</LastModified><Size>3</Size></Contents>`, url.PathEscape(keys[2]))
			}
			_, _ = io.WriteString(w, `</ListBucketResult>`)
			return
		}
		found := false
		for _, key := range keys {
			if r.URL.Path == "/bucket/"+key {
				found = true
			}
		}
		if !found {
			t.Errorf("read key changed: %q", r.URL.Path)
		}
		w.Header().Set("Content-Length", "3")
		_, _ = io.WriteString(w, "abc")
	})
	client := s3.New(s3.Options{Region: "us-east-1", Credentials: aws.AnonymousCredentials{}, BaseEndpoint: aws.String("https://example.test"), UsePathStyle: true, HTTPClient: encodingHTTPClient{handler: handler}})
	bucket, err := NewBucket(client, "bucket", object.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	objects, err := object.ListAll(t.Context(), bucket, "", object.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if len(objects) != len(keys) {
		t.Fatalf("got %d objects", len(objects))
	}
	for i, meta := range objects {
		if meta.Key != keys[i] {
			t.Fatalf("key=%q want=%q", meta.Key, keys[i])
		}
		got, err := bucket.Read(t.Context(), object.ReadRequest{Key: meta.Key})
		if err != nil {
			t.Fatal(err)
		}
		if string(got.Data) != "abc" {
			t.Fatalf("read=%+v", got)
		}
	}
}

type encodingHTTPClient struct{ handler http.Handler }

func (c encodingHTTPClient) Do(r *http.Request) (*http.Response, error) {
	recorder := httptest.NewRecorder()
	c.handler.ServeHTTP(recorder, r)
	return recorder.Result(), nil
}
