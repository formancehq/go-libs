package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"github.com/formancehq/go-libs/v5/pkg/storage"
	"github.com/formancehq/go-libs/v5/pkg/storage/internal/boundedio"
	"github.com/formancehq/go-libs/v5/pkg/storage/internal/objectio"
	"github.com/formancehq/go-libs/v5/pkg/storage/object"
)

type fakeS3 struct {
	get  func(*s3.GetObjectInput) (*s3.GetObjectOutput, error)
	list func(*s3.ListObjectsV2Input) (*s3.ListObjectsV2Output, error)
}

func (f fakeS3) GetObject(_ context.Context, in *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	return f.get(in)
}
func (f fakeS3) HeadObject(context.Context, *s3.HeadObjectInput, ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	return &s3.HeadObjectOutput{ContentLength: aws.Int64(3), ETag: aws.String("\"generation\""), LastModified: aws.Time(time.Unix(100, 0))}, nil
}
func (f fakeS3) ListObjectsV2(_ context.Context, in *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	return f.list(in)
}

type closingBody struct {
	io.Reader
	closed bool
}

func (b *closingBody) Close() error { b.closed = true; return nil }
func TestS3ConditionalBoundedReads(t *testing.T) {
	body := &closingBody{Reader: strings.NewReader("abc")}
	client := fakeS3{get: func(in *s3.GetObjectInput) (*s3.GetObjectOutput, error) {
		if aws.ToString(in.IfMatch) != "\"generation\"" || aws.ToString(in.Range) != "bytes=0-2" {
			t.Fatalf("request %+v", in)
		}
		return &s3.GetObjectOutput{Body: body, ContentLength: aws.Int64(3), ContentRange: aws.String("bytes 0-2/6"), ETag: aws.String("\"generation\"")}, nil
	}}
	bucket, err := NewBucket(client, "bucket", object.Limits{MaxBytes: 5})
	if err != nil {
		t.Fatal(err)
	}
	result, err := bucket.Read(t.Context(), object.ReadRequest{Key: "key", Revision: "\"generation\"", PrefixBytes: 3})
	if err != nil || result.Complete || string(result.Data) != "abc" || !body.closed {
		t.Fatalf("%+v %v closed=%v", result, err, body.closed)
	}
	if _, err := bucket.Read(t.Context(), object.ReadRequest{Key: "key", PrefixBytes: 6}); !errors.Is(err, object.ErrTooLarge) {
		t.Fatal(err)
	}
	bucket.client = fakeS3{get: func(*s3.GetObjectInput) (*s3.GetObjectOutput, error) {
		return nil, &smithy.GenericAPIError{Code: "PreconditionFailed"}
	}}
	if _, err := bucket.Read(t.Context(), object.ReadRequest{Key: "key", Revision: "v1"}); !errors.Is(err, object.ErrRevisionChanged) {
		t.Fatal(err)
	}
}
func TestS3RejectsOversizedBodiesAndBrokenListings(t *testing.T) {
	body := &closingBody{Reader: strings.NewReader("123456")}
	bucket, err := NewBucket(fakeS3{get: func(*s3.GetObjectInput) (*s3.GetObjectOutput, error) {
		return &s3.GetObjectOutput{Body: body, ContentLength: aws.Int64(6)}, nil
	}}, "bucket", object.Limits{MaxBytes: 3})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bucket.Read(t.Context(), object.ReadRequest{Key: "key"}); !errors.Is(err, object.ErrTooLarge) || !body.closed {
		t.Fatalf("%v closed=%v", err, body.closed)
	}
	bucket.client = fakeS3{list: func(*s3.ListObjectsV2Input) (*s3.ListObjectsV2Output, error) {
		return &s3.ListObjectsV2Output{IsTruncated: aws.Bool(true), NextContinuationToken: aws.String("same"), Contents: []types.Object{}}, nil
	}}
	if _, err := object.ListAll(t.Context(), bucket, "", object.Limits{}); !errors.Is(err, object.ErrListingLimit) {
		t.Fatal(err)
	}
	bucket.client = fakeS3{list: func(*s3.ListObjectsV2Input) (*s3.ListObjectsV2Output, error) {
		return &s3.ListObjectsV2Output{IsTruncated: aws.Bool(true)}, nil
	}}
	if _, err := bucket.ListPage(t.Context(), object.ListRequest{Limit: 2}); !errors.Is(err, object.ErrListingLimit) {
		t.Fatal(err)
	}
}
func TestBoundsAndTruncatedTransport(t *testing.T) {
	if _, err := object.NormalizeLimits(object.Limits{MaxBytes: math.MaxInt64}); !errors.Is(err, object.ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := objectio.ReadResponse(t.Context(), strings.NewReader("ab"), 3, "", object.ReadRequest{}, 3); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal(err)
	}
	if _, err := objectio.ReadResponse(t.Context(), strings.NewReader("abc"), 3, "bytes 1-3/6", object.ReadRequest{PrefixBytes: 3}, 3); !errors.Is(err, object.ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := boundedio.ReadBody(t.Context(), strings.NewReader("four"), 3); !errors.Is(err, object.ErrTooLarge) {
		t.Fatal(err)
	}
}

func TestCurrentGenerationAndMetadataRefusal(t *testing.T) {
	bucket, err := NewBucket(fakeS3{get: func(in *s3.GetObjectInput) (*s3.GetObjectOutput, error) {
		if aws.ToString(in.IfMatch) != "\"generation\"" || in.Range != nil {
			t.Fatal("current generation not pinned")
		}
		return &s3.GetObjectOutput{Body: io.NopCloser(strings.NewReader("abc")), ContentLength: aws.Int64(3), ETag: in.IfMatch}, nil
	}}, "bucket", object.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	metadata, data, err := object.ReadCurrent(t.Context(), bucket, "item", 3)
	if err != nil || metadata.Key != "item" || string(data) != "abc" {
		t.Fatalf("%+v %v", metadata, err)
	}
	if _, _, err := object.ReadCurrent(t.Context(), bucket, "item", 2); !errors.Is(err, object.ErrTooLarge) {
		t.Fatal(err)
	}
	if _, _, err := object.ReadCurrent(t.Context(), bucket, "item", 0); !errors.Is(err, object.ErrInvalid) {
		t.Fatal(err)
	}
	bucket.client = fakeS3{get: func(*s3.GetObjectInput) (*s3.GetObjectOutput, error) {
		return &s3.GetObjectOutput{Body: io.NopCloser(strings.NewReader("abc")), ContentLength: aws.Int64(3), ETag: aws.String("changed")}, nil
	}}
	if _, _, err := object.ReadCurrent(t.Context(), bucket, "item", 3); !errors.Is(err, object.ErrRevisionChanged) {
		t.Fatal(err)
	}
}
func TestS3ClassificationsPreserveCause(t *testing.T) {
	for _, tc := range []struct {
		code     string
		sentinel error
	}{{"PreconditionFailed", object.ErrRevisionChanged}, {"NoSuchKey", os.ErrNotExist}, {"InvalidAccessKeyId", storage.ErrCredentials}, {"AccessDenied", storage.ErrPermission}} {
		original := &smithy.GenericAPIError{Code: tc.code}
		wrapped := storageError(original)
		if !errors.Is(wrapped, tc.sentinel) || !errors.Is(wrapped, original) {
			t.Fatalf("classification lost %s", tc.code)
		}
	}
}

func TestCanceledRequestsNeverReachBackend(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	bucket, err := NewBucket(fakeS3{}, "bucket", object.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bucket.ListPage(ctx, object.ListRequest{Limit: 1}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := bucket.Stat(ctx, "item"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := bucket.Read(ctx, object.ReadRequest{Key: "item"}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := object.ListAll(ctx, bucket, "", object.Limits{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestLimitsRefuseInvalidAndEmptyAdvancingPages(t *testing.T) {
	for _, l := range []object.Limits{{MaxBytes: -1}, {MaxObjects: -1}, {MaxPages: -1}} {
		if _, err := NewBucket(fakeS3{}, "bucket", l); !errors.Is(err, object.ErrInvalid) {
			t.Fatal(err)
		}
	}
	if _, err := NewBucket(nil, "bucket", object.Limits{}); !errors.Is(err, object.ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := NewBucket(fakeS3{}, "", object.Limits{}); !errors.Is(err, object.ErrInvalid) {
		t.Fatal(err)
	}
	bucket, err := NewBucket(fakeS3{list: func(*s3.ListObjectsV2Input) (*s3.ListObjectsV2Output, error) {
		return &s3.ListObjectsV2Output{IsTruncated: aws.Bool(true), NextContinuationToken: aws.String("advance")}, nil
	}}, "bucket", object.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := object.ListAll(t.Context(), bucket, "", object.Limits{MaxPages: 1}); !errors.Is(err, object.ErrListingLimit) || got != nil {
		t.Fatalf("partial listing: %v %v", got, err)
	}
	for _, req := range []object.ReadRequest{{Key: ""}, {Key: "item", MaxBytes: -1}, {Key: "item", PrefixBytes: -1}} {
		if _, err := bucket.Read(t.Context(), req); !errors.Is(err, object.ErrInvalid) {
			t.Fatal(err)
		}
	}
}

func TestMissingBucketAndExactListingCeiling(t *testing.T) {
	if _, err := object.ListAll(t.Context(), nil, "", object.Limits{}); !errors.Is(err, object.ErrInvalid) {
		t.Fatal(err)
	}
	if _, _, err := object.ReadCurrent(t.Context(), nil, "key", 1); !errors.Is(err, object.ErrInvalid) {
		t.Fatal(err)
	}
	bucket, err := NewBucket(fakeS3{list: func(in *s3.ListObjectsV2Input) (*s3.ListObjectsV2Output, error) {
		if aws.ToInt32(in.MaxKeys) != 1 {
			t.Fatal("aggregate cap not propagated")
		}
		return &s3.ListObjectsV2Output{Contents: []types.Object{{Key: aws.String("item"), Size: aws.Int64(1), LastModified: aws.Time(time.Unix(1, 0))}}}, nil
	}}, "bucket", object.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := object.ListAll(t.Context(), bucket, "", object.Limits{MaxObjects: 1}); err != nil || len(got) != 1 {
		t.Fatalf("%v %v", got, err)
	}
}

func TestListAllCannotRaiseConfiguredAggregateBounds(t *testing.T) {
	calls := 0
	client := fakeS3{list: func(in *s3.ListObjectsV2Input) (*s3.ListObjectsV2Output, error) {
		calls++
		return &s3.ListObjectsV2Output{Contents: []types.Object{{Key: aws.String("item"), Size: aws.Int64(1), LastModified: aws.Time(time.Unix(1, 0))}}, IsTruncated: aws.Bool(true), NextContinuationToken: aws.String(fmt.Sprint(calls))}, nil
	}}
	for _, bound := range []object.Limits{{MaxObjects: 2}, {MaxPages: 2}} {
		calls = 0
		bucket, err := NewBucket(client, "bucket", bound)
		if err != nil {
			t.Fatal(err)
		}
		got, err := object.ListAll(t.Context(), bucket, "", object.Limits{})
		if !errors.Is(err, object.ErrListingLimit) || got != nil || calls != 2 {
			t.Fatalf("bounds %+v: got=%v err=%v calls=%d", bound, got, err, calls)
		}
	}
}
