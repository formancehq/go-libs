package oauth2introspect

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/dgraph-io/ristretto"
	"github.com/pkg/errors"
	"net/http"
	"net/url"
	"time"
)

type introspecter struct {
	introspectUrl string
	client        *http.Client
	cache         *ristretto.Cache
	cacheTTL      time.Duration
}

func (i *introspecter) Introspect(ctx context.Context, bearer string) (bool, error) {

	v, ok := i.cache.Get(bearer)
	if ok {
		return v.(bool), nil
	}

	form := url.Values{}
	form.Set("token", bearer)

	checkAuthReq, err := http.NewRequest(http.MethodPost, i.introspectUrl, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return false, errors.Wrap(err, "creating introspection request")
	}
	checkAuthReq.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	rsp, err := i.client.Do(checkAuthReq)
	if err != nil {
		return false, errors.Wrap(err, "making introspection request")
	}

	switch rsp.StatusCode {
	case http.StatusOK:
		type X struct {
			Active bool `json:"active"`
		}
		x := X{}
		err = json.NewDecoder(rsp.Body).Decode(&x)
		if err != nil {
			return false, errors.Wrap(err, "decoding introspection response")
		}

		_ = i.cache.SetWithTTL(bearer, x.Active, 1, i.cacheTTL)

		return x.Active, nil
	default:
		return false, fmt.Errorf("unexpected status code %d on introspection request", rsp.StatusCode)
	}
}

func NewIntrospecter(client *http.Client, cache *ristretto.Cache, url string, cacheTtl time.Duration) *introspecter {
	return &introspecter{
		introspectUrl: url,
		client:        client,
		cache:         cache,
		cacheTTL:      cacheTtl,
	}
}
