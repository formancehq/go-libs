package azureblob

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"

	"github.com/formancehq/go-libs/v5/pkg/storage/internal/boundedio"
	"github.com/formancehq/go-libs/v5/pkg/storage/internal/objectio"
	"github.com/formancehq/go-libs/v5/pkg/storage/object"
)

// Bucket accepts a native Azure SDK client composed using workload identity, shared key, SAS,
// or connection string. The bucket does not select or log credentials.
type Bucket struct {
	client    *azblob.Client
	container string
	limits    object.Limits
}

func NewBucket(client *azblob.Client, container string, bounds object.Limits) (*Bucket, error) {
	l, err := object.NormalizeLimits(bounds)
	if err != nil {
		return nil, err
	}
	if client == nil || container == "" {
		return nil, object.ErrInvalid
	}
	return &Bucket{client: client, container: container, limits: l}, nil
}
func (a *Bucket) ListPage(ctx context.Context, req object.ListRequest) (object.Page, error) {
	if err := ctx.Err(); err != nil {
		return object.Page{}, err
	}
	if req.Limit <= 0 || req.Limit > 5000 {
		return object.Page{}, object.ErrInvalid
	}
	req.Limit = min(req.Limit, a.limits.MaxObjects, 5000)
	count := int32(req.Limit)
	options := &azblob.ListBlobsFlatOptions{Prefix: &req.Prefix, Marker: &req.Cursor, MaxResults: &count}
	var raw *http.Response
	page, err := a.client.NewListBlobsFlatPager(a.container, options).NextPage(policy.WithCaptureResponse(ctx, &raw))
	if err != nil {
		return object.Page{}, azureError(err)
	}
	if page.Segment == nil || len(page.Segment.BlobItems) > req.Limit {
		return object.Page{}, object.ErrInvalid
	}
	out := object.Page{ServerTime: objectio.ServerTime(raw)}
	for _, item := range page.Segment.BlobItems {
		if item == nil || item.Name == nil || *item.Name == "" || item.Properties == nil || item.Properties.ContentLength == nil || *item.Properties.ContentLength < 0 || item.Properties.LastModified == nil {
			return object.Page{}, object.ErrInvalid
		}
		m := object.Metadata{Key: *item.Name, Size: *item.Properties.ContentLength, LastModified: item.Properties.LastModified.UTC()}
		if item.Properties.ETag != nil {
			m.Revision = string(*item.Properties.ETag)
		}
		out.Objects = append(out.Objects, m)
	}
	if page.NextMarker != nil {
		out.NextCursor = *page.NextMarker
	}
	if strings.TrimSpace(out.NextCursor) == "" {
		out.NextCursor = ""
	}
	if out.NextCursor != "" && out.NextCursor == req.Cursor {
		return object.Page{}, object.ErrListingLimit
	}
	return out, nil
}
func (a *Bucket) Stat(ctx context.Context, key string) (object.Metadata, error) {
	if err := ctx.Err(); err != nil {
		return object.Metadata{}, err
	}
	if key == "" {
		return object.Metadata{}, object.ErrInvalid
	}
	raw, err := a.client.ServiceClient().NewContainerClient(a.container).NewBlobClient(key).GetProperties(ctx, nil)
	if err != nil {
		return object.Metadata{}, azureError(err)
	}
	if raw.ContentLength == nil || *raw.ContentLength < 0 || raw.LastModified == nil {
		return object.Metadata{}, object.ErrInvalid
	}
	meta := object.Metadata{Key: key, Size: *raw.ContentLength, LastModified: raw.LastModified.UTC()}
	if raw.ETag != nil {
		meta.Revision = string(*raw.ETag)
	}
	return meta, nil
}
func (a *Bucket) Read(ctx context.Context, req object.ReadRequest) (object.ReadResult, error) {
	if err := ctx.Err(); err != nil {
		return object.ReadResult{}, err
	}
	cap, err := boundedio.ReadBound(req, a.limits.MaxBytes)
	if err != nil {
		return object.ReadResult{}, err
	}
	options := &azblob.DownloadStreamOptions{}
	if req.PrefixBytes > 0 {
		options.Range = blob.HTTPRange{Count: req.PrefixBytes}
	}
	if req.Revision != "" {
		tag := azcore.ETag(req.Revision)
		options.AccessConditions = &blob.AccessConditions{ModifiedAccessConditions: &blob.ModifiedAccessConditions{IfMatch: &tag}}
	}
	raw, err := a.client.DownloadStream(ctx, a.container, req.Key, options)
	if err != nil {
		return object.ReadResult{}, azureError(err)
	}
	if raw.Body == nil {
		return object.ReadResult{}, object.ErrInvalid
	}
	defer func() { _ = raw.Body.Close() }()
	if req.Revision != "" && (raw.ETag == nil || string(*raw.ETag) != req.Revision) {
		return object.ReadResult{}, object.ErrRevisionChanged
	}
	if raw.ContentLength == nil {
		return object.ReadResult{}, object.ErrInvalid
	}
	contentRange := ""
	if raw.ContentRange != nil {
		contentRange = *raw.ContentRange
	}
	return objectio.ReadResponse(ctx, raw.Body, *raw.ContentLength, contentRange, req, cap)
}
func azureError(err error) error {
	if response, ok := errors.AsType[*azcore.ResponseError](err); ok {
		switch response.StatusCode {
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
