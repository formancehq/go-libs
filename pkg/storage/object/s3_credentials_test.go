package object

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/formancehq/go-libs/v5/pkg/storage"
)

type answerTransport func(*http.Request) (*http.Response, error)

func (f answerTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// s3Answer is what the stub endpoint returns to every request: an S3 error
// body with code, or a transport failure when err is set.
type s3Answer struct {
	status int
	code   string
	err    error
}

// newAnsweringS3 drives the real SDK client, so the error code is decoded from
// the body by the SDK rather than built by hand. Retries are off: the SDK's own
// backoff for 5xx and throttling is not what these tests measure.
func newAnsweringS3(answer s3Answer) *S3 {
	client := s3.New(s3.Options{
		Region:       "auto",
		BaseEndpoint: aws.String("https://storage.example"),
		UsePathStyle: true,
		Credentials:  credentials.NewStaticCredentialsProvider("test-id", "test-secret", ""),
		Retryer:      aws.NopRetryer{},
		HTTPClient: &http.Client{Transport: answerTransport(func(request *http.Request) (*http.Response, error) {
			if answer.err != nil {
				return nil, answer.err
			}
			body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><Error><Code>%s</Code><Message>stub %s</Message><RequestId>req-1</RequestId></Error>`, answer.code, answer.code)
			if request.Method == http.MethodHead {
				// A HEAD response has no body, so S3 cannot say why it refused.
				body = ""
			}
			return &http.Response{
				StatusCode: answer.status,
				Header:     http.Header{"Content-Type": {"application/xml"}},
				Request:    request,
				Body:       io.NopCloser(strings.NewReader(body)),
			}, nil
		})},
	})

	bucket, err := NewS3(client, "archive", Limits{})
	if err != nil {
		panic(err)
	}
	return bucket
}

// s3Operations is every S3 call a object consumer makes to list or read.
var s3Operations = map[string]func(context.Context, *S3) error{
	"list": func(ctx context.Context, store *S3) error {
		_, err := ListAll(ctx, store, "objects/", Limits{})
		return err
	},
	"list page": func(ctx context.Context, store *S3) error {
		_, err := store.ListPage(ctx, ListRequest{Prefix: "objects/", Limit: 10})
		return err
	},
	"get": func(ctx context.Context, store *S3) error {
		_, err := store.Read(ctx, ReadRequest{Key: "objects/item"})
		return err
	},
	"get revision bounded": func(ctx context.Context, store *S3) error {
		_, err := store.Read(ctx, ReadRequest{Key: "objects/item", Revision: `"etag"`, MaxBytes: 1024})
		return err
	},
	"get range": func(ctx context.Context, store *S3) error {
		_, err := store.Read(ctx, ReadRequest{Key: "objects/item", PrefixBytes: 512})
		return err
	},
}

func TestS3MarksCredentialVerdictsOnListAndRead(t *testing.T) {
	for _, verdict := range []struct {
		status int
		code   string
		want   error
	}{
		{status: http.StatusForbidden, code: "InvalidAccessKeyId", want: storage.ErrCredentials},
		{status: http.StatusForbidden, code: "SignatureDoesNotMatch", want: storage.ErrCredentials},
		{status: http.StatusUnauthorized, code: "Unauthorized", want: storage.ErrCredentials},
		{status: http.StatusForbidden, code: "AccessDenied", want: storage.ErrPermission},
	} {
		for name, call := range s3Operations {
			t.Run(verdict.code+"/"+name, func(t *testing.T) {
				err := call(t.Context(), newAnsweringS3(s3Answer{status: verdict.status, code: verdict.code}))
				if !errors.Is(err, verdict.want) {
					t.Fatalf("err = %v, want it to carry %v", err, verdict.want)
				}
				// The operator reads the provider's own code off the halted source.
				if !strings.Contains(err.Error(), verdict.code) {
					t.Fatalf("verdict lost the provider code: %q", err.Error())
				}
			})
		}
	}
}

// Everything that is not a documented credential code stays unmarked, so
// callers can keep retrying it. Several of these are 403s: S3 classifies by
// code, never by status.
func TestS3LeavesOtherFailuresRetryable(t *testing.T) {
	for _, control := range []struct {
		name   string
		answer s3Answer
	}{
		{name: "throttled", answer: s3Answer{status: http.StatusServiceUnavailable, code: "SlowDown"}},
		{name: "outage", answer: s3Answer{status: http.StatusServiceUnavailable, code: "ServiceUnavailable"}},
		{name: "internal error", answer: s3Answer{status: http.StatusInternalServerError, code: "InternalError"}},
		{name: "request timeout", answer: s3Answer{status: http.StatusBadRequest, code: "RequestTimeout"}},
		{name: "clock skew", answer: s3Answer{status: http.StatusForbidden, code: "RequestTimeTooSkewed"}},
		{name: "expired signature", answer: s3Answer{status: http.StatusForbidden, code: "ExpiredRequest"}},
		{name: "session token mid-refresh", answer: s3Answer{status: http.StatusBadRequest, code: "ExpiredToken"}},
		{name: "archived object", answer: s3Answer{status: http.StatusForbidden, code: "InvalidObjectState"}},
		{name: "missing bucket", answer: s3Answer{status: http.StatusNotFound, code: "NoSuchBucket"}},
		{name: "missing key", answer: s3Answer{status: http.StatusNotFound, code: "NoSuchKey"}},
		{name: "unknown 403 code", answer: s3Answer{status: http.StatusForbidden, code: "SomethingNew"}},
		{name: "transport failure", answer: s3Answer{err: errors.New("dial tcp: connection refused")}},
		{name: "timeout", answer: s3Answer{err: context.DeadlineExceeded}},
	} {
		for name, call := range s3Operations {
			t.Run(control.name+"/"+name, func(t *testing.T) {
				err := call(t.Context(), newAnsweringS3(control.answer))
				if err == nil {
					t.Fatal("expected the call to fail")
				}
				if errors.Is(err, storage.ErrCredentials) || errors.Is(err, storage.ErrPermission) {
					t.Fatalf("retryable failure marked as a credential verdict: %v", err)
				}
			})
		}
	}
}

// A HEAD 403 carries no body, so it cannot tell a revoked key from clock skew:
// Stat leaves it unmarked.
func TestS3StatLeavesBodilessForbiddenRetryable(t *testing.T) {
	_, err := newAnsweringS3(s3Answer{status: http.StatusForbidden, code: "AccessDenied"}).Stat(t.Context(), "objects/item")
	if err == nil {
		t.Fatal("expected Stat to fail")
	}
	if errors.Is(err, storage.ErrCredentials) || errors.Is(err, storage.ErrPermission) {
		t.Fatalf("bodiless HEAD 403 marked as a credential verdict: %v", err)
	}
}
