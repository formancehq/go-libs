package s3

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsmiddleware "github.com/aws/aws-sdk-go-v2/aws/middleware"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	"github.com/formancehq/go-libs/v5/pkg/storage"
	"github.com/formancehq/go-libs/v5/pkg/storage/internal/boundedio"
	"github.com/formancehq/go-libs/v5/pkg/storage/internal/objectio"
	"github.com/formancehq/go-libs/v5/pkg/storage/object"
)

// BucketClient is implemented directly by the AWS SDK client. Client configuration,
// credential resolution and endpoint/path-style selection remain at composition.
type BucketClient interface {
	ListObjectsV2(context.Context, *s3.ListObjectsV2Input, ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	HeadObject(context.Context, *s3.HeadObjectInput, ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
}

type Bucket struct {
	client BucketClient
	bucket string
	limits object.Limits
}

func NewBucket(client BucketClient, bucket string, bounds object.Limits) (*Bucket, error) {
	l, err := object.NormalizeLimits(bounds)
	if err != nil {
		return nil, err
	}
	if client == nil || bucket == "" {
		return nil, object.ErrInvalid
	}
	return &Bucket{client: client, bucket: bucket, limits: l}, nil
}
func (s *Bucket) ListPage(ctx context.Context, req object.ListRequest) (object.Page, error) {
	if err := ctx.Err(); err != nil {
		return object.Page{}, err
	}
	if req.Limit <= 0 || req.Limit > 1000 {
		return object.Page{}, object.ErrInvalid
	}
	req.Limit = min(req.Limit, s.limits.MaxObjects, 1000)
	in := &s3.ListObjectsV2Input{Bucket: aws.String(s.bucket), Prefix: aws.String(req.Prefix), MaxKeys: aws.Int32(int32(req.Limit)), EncodingType: types.EncodingTypeUrl}
	if req.Cursor != "" {
		in.ContinuationToken = aws.String(req.Cursor)
	}
	raw, err := s.client.ListObjectsV2(ctx, in)
	if err != nil {
		return object.Page{}, storageError(err)
	}
	if raw == nil || len(raw.Contents) > req.Limit {
		return object.Page{}, object.ErrInvalid
	}
	out := object.Page{}
	if raw.EncodingType != "" && raw.EncodingType != types.EncodingTypeUrl {
		return object.Page{}, object.ErrInvalid
	}
	for _, item := range raw.Contents {
		if item.Key == nil || *item.Key == "" || item.Size == nil || *item.Size < 0 || item.LastModified == nil {
			return object.Page{}, object.ErrInvalid
		}
		key := aws.ToString(item.Key)
		if raw.EncodingType == types.EncodingTypeUrl {
			key, err = url.PathUnescape(key)
			if err != nil || key == "" {
				return object.Page{}, object.ErrInvalid
			}
		}
		out.Objects = append(out.Objects, object.Metadata{Key: key, Size: aws.ToInt64(item.Size), LastModified: aws.ToTime(item.LastModified).UTC(), Revision: aws.ToString(item.ETag)})
	}
	if aws.ToBool(raw.IsTruncated) {
		out.NextCursor = aws.ToString(raw.NextContinuationToken)
		if out.NextCursor == "" || out.NextCursor == req.Cursor {
			return object.Page{}, object.ErrListingLimit
		}
	}
	if rawResponse, ok := awsmiddleware.GetRawResponse(raw.ResultMetadata).(*smithyhttp.Response); ok {
		out.ServerTime = objectio.ServerTime(rawResponse.Response)
	}
	return out, nil
}
func (s *Bucket) Stat(ctx context.Context, key string) (object.Metadata, error) {
	if err := ctx.Err(); err != nil {
		return object.Metadata{}, err
	}
	if key == "" {
		return object.Metadata{}, object.ErrInvalid
	}
	raw, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return object.Metadata{}, err
	}
	if raw == nil || raw.ContentLength == nil || *raw.ContentLength < 0 || raw.LastModified == nil {
		return object.Metadata{}, object.ErrInvalid
	}
	return object.Metadata{Key: key, Size: *raw.ContentLength, LastModified: raw.LastModified.UTC(), Revision: aws.ToString(raw.ETag)}, nil
}
func (s *Bucket) Read(ctx context.Context, req object.ReadRequest) (object.ReadResult, error) {
	if err := ctx.Err(); err != nil {
		return object.ReadResult{}, err
	}
	cap, err := boundedio.ReadBound(req, s.limits.MaxBytes)
	if err != nil {
		return object.ReadResult{}, err
	}
	in := &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(req.Key)}
	if req.Revision != "" {
		in.IfMatch = aws.String(req.Revision)
	}
	if req.PrefixBytes > 0 {
		in.Range = aws.String(fmt.Sprintf("bytes=0-%d", req.PrefixBytes-1))
	}
	raw, err := s.client.GetObject(ctx, in)
	if err != nil {
		return object.ReadResult{}, storageError(err)
	}
	if raw == nil || raw.Body == nil {
		return object.ReadResult{}, object.ErrInvalid
	}
	defer func() { _ = raw.Body.Close() }()
	if req.Revision != "" && aws.ToString(raw.ETag) != req.Revision {
		return object.ReadResult{}, object.ErrRevisionChanged
	}
	if raw.ContentLength == nil {
		return object.ReadResult{}, object.ErrInvalid
	}
	return objectio.ReadResponse(ctx, raw.Body, aws.ToInt64(raw.ContentLength), aws.ToString(raw.ContentRange), req, cap)
}

func storageError(err error) error {
	if api, ok := errors.AsType[smithy.APIError](err); ok {
		switch api.ErrorCode() {
		case "PreconditionFailed":
			return errors.Join(object.ErrRevisionChanged, err)
		case "NoSuchKey", "NotFound":
			return errors.Join(os.ErrNotExist, err)
		case "InvalidAccessKeyId", "SignatureDoesNotMatch", "Unauthorized":
			return errors.Join(storage.ErrCredentials, err)
		case "AccessDenied":
			return errors.Join(storage.ErrPermission, err)
		}
	}
	if status, ok := errors.AsType[interface {
		error
		HTTPStatusCode() int
	}](err); ok {
		switch status.HTTPStatusCode() {
		case http.StatusPreconditionFailed:
			return errors.Join(object.ErrRevisionChanged, err)
		case http.StatusNotFound:
			return errors.Join(os.ErrNotExist, err)
		}
	}
	return err
}

// Bounds returns the configured normalized ceilings.
func (b *Bucket) Bounds() object.Limits { return b.limits }

var _ object.Bucket = (*Bucket)(nil)
