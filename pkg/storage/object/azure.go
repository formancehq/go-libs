package object

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
)

// Azure accepts an SDK client composed using workload identity, shared key, SAS,
// or connection string. The bucket does not select or log credentials.
type Azure struct {
	client    *azblob.Client
	container string
	limits    Limits
}

func NewAzure(client *azblob.Client, container string, bounds Limits) (*Azure, error) {
	l, err := limits(bounds)
	if err != nil {
		return nil, err
	}
	if client == nil || container == "" {
		return nil, ErrInvalid
	}
	return &Azure{client: client, container: container, limits: l}, nil
}
func (a *Azure) ListPage(ctx context.Context, req ListRequest) (Page, error) {
	if err := ctx.Err(); err != nil {
		return Page{}, err
	}
	if req.Limit <= 0 || req.Limit > 5000 {
		return Page{}, ErrInvalid
	}
	req.Limit = min(req.Limit, a.limits.MaxObjects, 5000)
	count := int32(req.Limit)
	options := &azblob.ListBlobsFlatOptions{Prefix: &req.Prefix, Marker: &req.Cursor, MaxResults: &count}
	var raw *http.Response
	page, err := a.client.NewListBlobsFlatPager(a.container, options).NextPage(policy.WithCaptureResponse(ctx, &raw))
	if err != nil {
		return Page{}, azureError(err)
	}
	if page.Segment == nil || len(page.Segment.BlobItems) > req.Limit {
		return Page{}, ErrInvalid
	}
	out := Page{ServerTime: serverTime(raw)}
	for _, item := range page.Segment.BlobItems {
		if item == nil || item.Name == nil || *item.Name == "" || item.Properties == nil || item.Properties.ContentLength == nil || *item.Properties.ContentLength < 0 || item.Properties.LastModified == nil {
			return Page{}, ErrInvalid
		}
		m := Metadata{Key: *item.Name, Size: *item.Properties.ContentLength, LastModified: item.Properties.LastModified.UTC()}
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
		return Page{}, ErrListingLimit
	}
	return out, nil
}
func (a *Azure) Stat(ctx context.Context, key string) (Metadata, error) {
	if err := ctx.Err(); err != nil {
		return Metadata{}, err
	}
	if key == "" {
		return Metadata{}, ErrInvalid
	}
	raw, err := a.client.ServiceClient().NewContainerClient(a.container).NewBlobClient(key).GetProperties(ctx, nil)
	if err != nil {
		return Metadata{}, azureError(err)
	}
	if raw.ContentLength == nil || *raw.ContentLength < 0 || raw.LastModified == nil || raw.ETag == nil {
		return Metadata{}, ErrInvalid
	}
	return Metadata{Key: key, Size: *raw.ContentLength, LastModified: raw.LastModified.UTC(), Revision: string(*raw.ETag)}, nil
}
func (a *Azure) Read(ctx context.Context, req ReadRequest) (ReadResult, error) {
	if err := ctx.Err(); err != nil {
		return ReadResult{}, err
	}
	cap, err := readBound(req, a.limits.MaxBytes)
	if err != nil {
		return ReadResult{}, err
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
		return ReadResult{}, azureError(err)
	}
	if raw.Body == nil {
		return ReadResult{}, ErrInvalid
	}
	defer func() { _ = raw.Body.Close() }()
	if req.Revision != "" && (raw.ETag == nil || string(*raw.ETag) != req.Revision) {
		return ReadResult{}, ErrRevisionChanged
	}
	if raw.ContentLength == nil {
		return ReadResult{}, ErrInvalid
	}
	contentRange := ""
	if raw.ContentRange != nil {
		contentRange = *raw.ContentRange
	}
	return readResponse(ctx, raw.Body, *raw.ContentLength, contentRange, req, cap)
}
func azureError(err error) error {
	if response, ok := errors.AsType[*azcore.ResponseError](err); ok {
		switch response.StatusCode {
		case http.StatusPreconditionFailed:
			return errors.Join(ErrRevisionChanged, err)
		case http.StatusNotFound:
			return errors.Join(os.ErrNotExist, err)
		}
	}
	return err
}
func serverTime(raw *http.Response) time.Time {
	if raw == nil || raw.Header.Get("Age") != "" {
		return time.Time{}
	}
	dates := raw.Header.Values("Date")
	if len(dates) != 1 {
		return time.Time{}
	}
	t, err := http.ParseTime(dates[0])
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}
