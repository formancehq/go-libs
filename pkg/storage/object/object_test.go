package object

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"github.com/formancehq/go-libs/v5/pkg/storage"
)

func TestDirectorySafety(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "item"), []byte("abcdef"), 0600); err != nil {
		t.Fatal(err)
	}
	d, err := OpenDirectory(root, Limits{MaxBytes: 6})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	m, err := d.Stat(t.Context(), "item")
	if err != nil {
		t.Fatal(err)
	}
	full, err := d.Read(t.Context(), ReadRequest{Key: "item", Revision: m.Revision})
	if err != nil || !full.Complete || string(full.Data) != "abcdef" {
		t.Fatalf("full read: %+v %v", full, err)
	}
	prefix, err := d.Read(t.Context(), ReadRequest{Key: "item", PrefixBytes: 3})
	if err != nil || prefix.Complete || string(prefix.Data) != "abc" {
		t.Fatalf("prefix: %+v %v", prefix, err)
	}
	if _, err := d.Read(t.Context(), ReadRequest{Key: "item", MaxBytes: 2}); !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
	if _, err := d.Read(t.Context(), ReadRequest{Key: "item", Revision: "changed"}); !errors.Is(err, ErrRevisionChanged) {
		t.Fatal(err)
	}
	for _, key := range []string{"../outside", "/absolute", `..\outside`, "."} {
		if _, err := d.Read(t.Context(), ReadRequest{Key: key}); err == nil {
			t.Fatalf("accepted %s", key)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := d.Read(ctx, ReadRequest{Key: "item"}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, _, err := ReadCurrent(t.Context(), d, "item", 6); err != nil {
		t.Fatal(err)
	}
}
func TestDirectoryContainmentAndPaging(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skip("symlinks unavailable")
	}
	for _, key := range []string{"one", "two", "three"} {
		if err := os.WriteFile(filepath.Join(root, key), []byte(key), 0600); err != nil {
			t.Fatal(err)
		}
	}
	d, err := OpenDirectory(root, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	if _, err := d.Read(t.Context(), ReadRequest{Key: "escape"}); err == nil {
		t.Fatal("read outside root")
	}
	got, err := ListAll(t.Context(), d, "", Limits{})
	if err != nil || len(got) != 3 {
		t.Fatalf("%v %v", got, err)
	}
	first, err := d.ListPage(t.Context(), ListRequest{Limit: 1})
	if err != nil || len(first.Objects) != 1 || first.NextCursor == "" {
		t.Fatalf("%+v %v", first, err)
	}
	second, err := d.ListPage(t.Context(), ListRequest{Limit: 1, Cursor: first.NextCursor})
	if err != nil || len(second.Objects) != 1 || second.Objects[0].Key == first.Objects[0].Key {
		t.Fatalf("%+v %v", second, err)
	}
	if _, err := ListAll(t.Context(), d, "", Limits{MaxObjects: 2}); !errors.Is(err, ErrListingLimit) {
		t.Fatal(err)
	}
}

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
	bucket, err := NewS3(client, "bucket", Limits{MaxBytes: 5})
	if err != nil {
		t.Fatal(err)
	}
	result, err := bucket.Read(t.Context(), ReadRequest{Key: "key", Revision: "\"generation\"", PrefixBytes: 3})
	if err != nil || result.Complete || string(result.Data) != "abc" || !body.closed {
		t.Fatalf("%+v %v closed=%v", result, err, body.closed)
	}
	if _, err := bucket.Read(t.Context(), ReadRequest{Key: "key", PrefixBytes: 6}); !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
	bucket.client = fakeS3{get: func(*s3.GetObjectInput) (*s3.GetObjectOutput, error) {
		return nil, &smithy.GenericAPIError{Code: "PreconditionFailed"}
	}}
	if _, err := bucket.Read(t.Context(), ReadRequest{Key: "key", Revision: "v1"}); !errors.Is(err, ErrRevisionChanged) {
		t.Fatal(err)
	}
}
func TestS3RejectsOversizedBodiesAndBrokenListings(t *testing.T) {
	body := &closingBody{Reader: strings.NewReader("123456")}
	bucket, err := NewS3(fakeS3{get: func(*s3.GetObjectInput) (*s3.GetObjectOutput, error) {
		return &s3.GetObjectOutput{Body: body, ContentLength: aws.Int64(6)}, nil
	}}, "bucket", Limits{MaxBytes: 3})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bucket.Read(t.Context(), ReadRequest{Key: "key"}); !errors.Is(err, ErrTooLarge) || !body.closed {
		t.Fatalf("%v closed=%v", err, body.closed)
	}
	bucket.client = fakeS3{list: func(*s3.ListObjectsV2Input) (*s3.ListObjectsV2Output, error) {
		return &s3.ListObjectsV2Output{IsTruncated: aws.Bool(true), NextContinuationToken: aws.String("same"), Contents: []types.Object{}}, nil
	}}
	if _, err := ListAll(t.Context(), bucket, "", Limits{}); !errors.Is(err, ErrListingLimit) {
		t.Fatal(err)
	}
	bucket.client = fakeS3{list: func(*s3.ListObjectsV2Input) (*s3.ListObjectsV2Output, error) {
		return &s3.ListObjectsV2Output{IsTruncated: aws.Bool(true)}, nil
	}}
	if _, err := bucket.ListPage(t.Context(), ListRequest{Limit: 2}); !errors.Is(err, ErrListingLimit) {
		t.Fatal(err)
	}
}
func TestBoundsAndTruncatedTransport(t *testing.T) {
	if _, err := limits(Limits{MaxBytes: math.MaxInt64}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := readResponse(t.Context(), strings.NewReader("ab"), 3, "", ReadRequest{}, 3); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal(err)
	}
	if _, err := readResponse(t.Context(), strings.NewReader("abc"), 3, "bytes 1-3/6", ReadRequest{PrefixBytes: 3}, 3); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := readBody(t.Context(), strings.NewReader("four"), 3); !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
}

func TestCurrentGenerationAndMetadataRefusal(t *testing.T) {
	bucket, err := NewS3(fakeS3{get: func(in *s3.GetObjectInput) (*s3.GetObjectOutput, error) {
		if aws.ToString(in.IfMatch) != "\"generation\"" || in.Range != nil {
			t.Fatal("current generation not pinned")
		}
		return &s3.GetObjectOutput{Body: io.NopCloser(strings.NewReader("abc")), ContentLength: aws.Int64(3), ETag: in.IfMatch}, nil
	}}, "bucket", Limits{})
	if err != nil {
		t.Fatal(err)
	}
	metadata, data, err := ReadCurrent(t.Context(), bucket, "item", 3)
	if err != nil || metadata.Key != "item" || string(data) != "abc" {
		t.Fatalf("%+v %v", metadata, err)
	}
	if _, _, err := ReadCurrent(t.Context(), bucket, "item", 2); !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
	if _, _, err := ReadCurrent(t.Context(), bucket, "item", 0); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	bucket.client = fakeS3{get: func(*s3.GetObjectInput) (*s3.GetObjectOutput, error) {
		return &s3.GetObjectOutput{Body: io.NopCloser(strings.NewReader("abc")), ContentLength: aws.Int64(3), ETag: aws.String("changed")}, nil
	}}
	if _, _, err := ReadCurrent(t.Context(), bucket, "item", 3); !errors.Is(err, ErrRevisionChanged) {
		t.Fatal(err)
	}
}
func TestDirectoryRejectsChangedGenerationAndSpecialFiles(t *testing.T) {
	root := t.TempDir()
	filename := filepath.Join(root, "item")
	if err := os.WriteFile(filename, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	d, err := OpenDirectory(root, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	meta, err := d.Stat(t.Context(), "item")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte("longer"), 0600); err != nil {
		t.Fatal(err)
	}
	if result, err := d.Read(t.Context(), ReadRequest{Key: "item", Revision: meta.Revision}); !errors.Is(err, ErrRevisionChanged) || result.Data != nil {
		t.Fatalf("changed content escaped: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "directory"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Stat(t.Context(), "directory"); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := d.Read(t.Context(), ReadRequest{Key: "directory"}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := d.Stat(t.Context(), "../item"); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := d.ListPage(t.Context(), ListRequest{Limit: 1, Cursor: "-1"}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}
func TestS3ClassificationsPreserveCause(t *testing.T) {
	for _, tc := range []struct {
		code     string
		sentinel error
	}{{"PreconditionFailed", ErrRevisionChanged}, {"NoSuchKey", os.ErrNotExist}, {"InvalidAccessKeyId", storage.ErrCredentials}, {"AccessDenied", storage.ErrPermission}} {
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
	bucket, err := NewS3(fakeS3{}, "bucket", Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bucket.ListPage(ctx, ListRequest{Limit: 1}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := bucket.Stat(ctx, "item"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := bucket.Read(ctx, ReadRequest{Key: "item"}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := ListAll(ctx, bucket, "", Limits{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestLimitsRefuseInvalidAndEmptyAdvancingPages(t *testing.T) {
	for _, l := range []Limits{{MaxBytes: -1}, {MaxObjects: -1}, {MaxPages: -1}} {
		if _, err := NewS3(fakeS3{}, "bucket", l); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
	}
	if _, err := NewS3(nil, "bucket", Limits{}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := NewS3(fakeS3{}, "", Limits{}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	bucket, err := NewS3(fakeS3{list: func(*s3.ListObjectsV2Input) (*s3.ListObjectsV2Output, error) {
		return &s3.ListObjectsV2Output{IsTruncated: aws.Bool(true), NextContinuationToken: aws.String("advance")}, nil
	}}, "bucket", Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := ListAll(t.Context(), bucket, "", Limits{MaxPages: 1}); !errors.Is(err, ErrListingLimit) || got != nil {
		t.Fatalf("partial listing: %v %v", got, err)
	}
	for _, req := range []ReadRequest{{Key: ""}, {Key: "item", MaxBytes: -1}, {Key: "item", PrefixBytes: -1}} {
		if _, err := bucket.Read(t.Context(), req); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
	}
}

func TestMissingBucketAndExactListingCeiling(t *testing.T) {
	if _, err := ListAll(t.Context(), nil, "", Limits{}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, _, err := ReadCurrent(t.Context(), nil, "key", 1); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	bucket, err := NewS3(fakeS3{list: func(in *s3.ListObjectsV2Input) (*s3.ListObjectsV2Output, error) {
		if aws.ToInt32(in.MaxKeys) != 1 {
			t.Fatal("aggregate cap not propagated")
		}
		return &s3.ListObjectsV2Output{Contents: []types.Object{{Key: aws.String("item"), Size: aws.Int64(1), LastModified: aws.Time(time.Unix(1, 0))}}}, nil
	}}, "bucket", Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := ListAll(t.Context(), bucket, "", Limits{MaxObjects: 1}); err != nil || len(got) != 1 {
		t.Fatalf("%v %v", got, err)
	}
}

func TestDirectoryRetainsRootAcrossRenameAndFiltersNestedKeys(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "item"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	directory, err := OpenDirectory(root, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = directory.Close() }()
	page, err := directory.ListPage(t.Context(), ListRequest{Prefix: "nested/", Limit: 2})
	if err != nil || len(page.Objects) != 1 || page.Objects[0].Key != "nested/item" {
		t.Fatalf("%+v %v", page, err)
	}
	if err := os.Rename(root, filepath.Join(base, "retained")); err != nil {
		t.Skip("platform cannot rename an open root")
	}
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "item"), []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := directory.Read(t.Context(), ReadRequest{Key: "nested/item"})
	if err != nil || string(result.Data) != "original" {
		t.Fatalf("configured root switched: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := directory.Stat(ctx, "nested/item"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := directory.ListPage(ctx, ListRequest{Limit: 1}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestLazyDirectoryFollowsConfiguredPathAndClosesOperations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "later")
	bucket, err := NewDirectory(path, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bucket.Stat(t.Context(), "item"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "item"), []byte("one"), 0600); err != nil {
		t.Fatal(err)
	}
	first, err := bucket.Read(t.Context(), ReadRequest{Key: "item"})
	if err != nil || string(first.Data) != "one" {
		t.Fatalf("%+v %v", first, err)
	}
	if err := os.Rename(path, path+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "item"), []byte("two"), 0600); err != nil {
		t.Fatal(err)
	}
	second, err := bucket.Read(t.Context(), ReadRequest{Key: "item"})
	if err != nil || string(second.Data) != "two" {
		t.Fatalf("%+v %v", second, err)
	}
	if err := bucket.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestListAllCannotRaiseConfiguredAggregateBounds(t *testing.T) {
	calls := 0
	client := fakeS3{list: func(in *s3.ListObjectsV2Input) (*s3.ListObjectsV2Output, error) {
		calls++
		return &s3.ListObjectsV2Output{Contents: []types.Object{{Key: aws.String("item"), Size: aws.Int64(1), LastModified: aws.Time(time.Unix(1, 0))}}, IsTruncated: aws.Bool(true), NextContinuationToken: aws.String(fmt.Sprint(calls))}, nil
	}}
	for _, bound := range []Limits{{MaxObjects: 2}, {MaxPages: 2}} {
		calls = 0
		bucket, err := NewS3(client, "bucket", bound)
		if err != nil {
			t.Fatal(err)
		}
		got, err := ListAll(t.Context(), bucket, "", Limits{})
		if !errors.Is(err, ErrListingLimit) || got != nil || calls != 2 {
			t.Fatalf("bounds %+v: got=%v err=%v calls=%d", bound, got, err, calls)
		}
	}
}

func TestDirectoryChunkedListingPreservesWireMetadataAndUTC(t *testing.T) {
	root := t.TempDir()
	modified := time.Date(2001, 2, 3, 4, 5, 6, 7, time.FixedZone("offset", 3600))
	for i := range 300 {
		file := filepath.Join(root, fmt.Sprintf("item-%03d", i))
		if err := os.WriteFile(file, []byte("abc"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(file, modified, modified); err != nil {
			t.Fatal(err)
		}
	}
	bucket, err := NewDirectory(root, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := ListAll(t.Context(), bucket, "item-", Limits{})
	if err != nil || len(got) != 300 {
		t.Fatalf("%d %v", len(got), err)
	}
	for _, meta := range got {
		if !meta.LastModified.Equal(modified) || meta.LastModified.Location() != time.UTC || meta.Size != 3 || meta.Revision != fmt.Sprintf("%d:3", modified.UnixNano()) {
			t.Fatalf("metadata changed: %+v", meta)
		}
	}
	raw, err := json.Marshal(got[0])
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf(`{"key":%q,"lastModified":%q,"size":3,"revision":%q}`, got[0].Key, modified.UTC().Format(time.RFC3339Nano), got[0].Revision)
	if string(raw) != want {
		t.Fatalf("wire metadata changed: %s, want %s", raw, want)
	}
	if _, err := ListAll(t.Context(), bucket, "item-", Limits{MaxObjects: 299}); !errors.Is(err, ErrListingLimit) {
		t.Fatal(err)
	}
}
