package object

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsmiddleware "github.com/aws/aws-sdk-go-v2/aws/middleware"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// S3Client is implemented directly by the AWS SDK client. Client configuration,
// credential resolution and endpoint/path-style selection remain at composition.
type S3Client interface {
	ListObjectsV2(context.Context, *s3.ListObjectsV2Input, ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	HeadObject(context.Context, *s3.HeadObjectInput, ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
}

type S3 struct {
	client S3Client
	bucket string
	limits Limits
}

func NewS3(client S3Client, bucket string, bounds Limits) (*S3, error) {
	l, err := limits(bounds)
	if err != nil {
		return nil, err
	}
	if client == nil || bucket == "" {
		return nil, ErrInvalid
	}
	return &S3{client: client, bucket: bucket, limits: l}, nil
}
func (s *S3) ListPage(ctx context.Context, req ListRequest) (Page, error) {
	if err := ctx.Err(); err != nil {
		return Page{}, err
	}
	if req.Limit <= 0 {
		return Page{}, ErrInvalid
	}
	req.Limit = min(req.Limit, s.limits.MaxObjects, 1000)
	in := &s3.ListObjectsV2Input{Bucket: aws.String(s.bucket), Prefix: aws.String(req.Prefix), MaxKeys: aws.Int32(int32(req.Limit))}
	if req.Cursor != "" {
		in.ContinuationToken = aws.String(req.Cursor)
	}
	raw, err := s.client.ListObjectsV2(ctx, in)
	if err != nil {
		return Page{}, storageError(err)
	}
	if raw == nil || len(raw.Contents) > req.Limit {
		return Page{}, ErrInvalid
	}
	out := Page{}
	for _, item := range raw.Contents {
		if item.Key == nil || *item.Key == "" || item.Size == nil || *item.Size < 0 || item.LastModified == nil {
			return Page{}, ErrInvalid
		}
		out.Objects = append(out.Objects, Metadata{Key: aws.ToString(item.Key), Size: aws.ToInt64(item.Size), LastModified: aws.ToTime(item.LastModified).UTC(), Revision: aws.ToString(item.ETag)})
	}
	if aws.ToBool(raw.IsTruncated) {
		out.NextCursor = aws.ToString(raw.NextContinuationToken)
		if out.NextCursor == "" || out.NextCursor == req.Cursor {
			return Page{}, ErrListingLimit
		}
	}
	if rawResponse, ok := awsmiddleware.GetRawResponse(raw.ResultMetadata).(*smithyhttp.Response); ok {
		out.ServerTime = serverTime(rawResponse.Response)
	}
	return out, nil
}
func (s *S3) Stat(ctx context.Context, key string) (Metadata, error) {
	if err := ctx.Err(); err != nil {
		return Metadata{}, err
	}
	if key == "" {
		return Metadata{}, ErrInvalid
	}
	raw, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return Metadata{}, storageError(err)
	}
	if raw == nil || raw.ContentLength == nil || *raw.ContentLength < 0 || raw.LastModified == nil {
		return Metadata{}, ErrInvalid
	}
	return Metadata{Key: key, Size: *raw.ContentLength, LastModified: raw.LastModified.UTC(), Revision: aws.ToString(raw.ETag)}, nil
}
func (s *S3) Read(ctx context.Context, req ReadRequest) (ReadResult, error) {
	if err := ctx.Err(); err != nil {
		return ReadResult{}, err
	}
	cap, err := readBound(req, s.limits.MaxBytes)
	if err != nil {
		return ReadResult{}, err
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
		return ReadResult{}, storageError(err)
	}
	if raw == nil || raw.Body == nil {
		return ReadResult{}, ErrInvalid
	}
	defer func() { _ = raw.Body.Close() }()
	if req.Revision != "" && aws.ToString(raw.ETag) != req.Revision {
		return ReadResult{}, ErrRevisionChanged
	}
	if raw.ContentLength == nil {
		return ReadResult{}, ErrInvalid
	}
	return readResponse(ctx, raw.Body, aws.ToInt64(raw.ContentLength), aws.ToString(raw.ContentRange), req, cap)
}

// readResponse validates a leading range rather than trusting its byte count.
func readResponse(ctx context.Context, body io.Reader, length int64, contentRange string, req ReadRequest, cap int64) (ReadResult, error) {
	if length < 0 {
		return ReadResult{}, ErrInvalid
	}
	if length > cap {
		return ReadResult{}, ErrTooLarge
	}
	data, err := readBody(ctx, body, cap)
	if err != nil {
		return ReadResult{}, err
	}
	if int64(len(data)) != length {
		return ReadResult{}, io.ErrUnexpectedEOF
	}
	complete := true
	if contentRange != "" {
		var last, total int64
		if !strings.HasPrefix(contentRange, "bytes 0-") {
			return ReadResult{}, ErrInvalid
		}
		parts := strings.Split(strings.TrimPrefix(contentRange, "bytes 0-"), "/")
		if len(parts) != 2 {
			return ReadResult{}, ErrInvalid
		}
		last, err = strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return ReadResult{}, ErrInvalid
		}
		total, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil || total < 0 || last < 0 || last >= total || last != int64(len(data))-1 {
			return ReadResult{}, ErrInvalid
		}
		complete = last == total-1
	} else if req.PrefixBytes > 0 && int64(len(data)) == req.PrefixBytes {
		// A full-size body without Content-Range could be an unqualified prefix.
		complete = false
	}
	if req.PrefixBytes == 0 && !complete {
		return ReadResult{}, ErrInvalid
	}
	return ReadResult{Data: data, Complete: complete}, nil
}

var (
	ErrCredentials = errors.New("object: credentials rejected")
	ErrPermission  = errors.New("object: access denied")
)

func storageError(err error) error {
	if api, ok := errors.AsType[smithy.APIError](err); ok {
		switch api.ErrorCode() {
		case "PreconditionFailed":
			return errors.Join(ErrRevisionChanged, err)
		case "NoSuchKey", "NotFound":
			return errors.Join(os.ErrNotExist, err)
		case "InvalidAccessKeyId", "SignatureDoesNotMatch", "ExpiredToken", "InvalidToken", "Unauthorized":
			return errors.Join(ErrCredentials, err)
		case "AccessDenied":
			return errors.Join(ErrPermission, err)
		}
	}
	if status, ok := errors.AsType[interface {
		error
		HTTPStatusCode() int
	}](err); ok {
		switch status.HTTPStatusCode() {
		case http.StatusPreconditionFailed:
			return errors.Join(ErrRevisionChanged, err)
		case http.StatusNotFound:
			return errors.Join(os.ErrNotExist, err)
		case http.StatusUnauthorized:
			return errors.Join(ErrCredentials, err)
		case http.StatusForbidden:
			return errors.Join(ErrPermission, err)
		}
	}
	return err
}
