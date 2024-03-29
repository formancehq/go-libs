package analytics

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/segmentio/analytics-go"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
)

type transport func(*http.Request) (*http.Response, error)

func (fn transport) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

type Queue[ITEM any] struct {
	mu    sync.Mutex
	items []ITEM
}

func (s *Queue[ITEM]) Get() (ret ITEM, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.items) == 0 {
		return
	}
	ret = s.items[0]
	ok = true
	if len(s.items) == 1 {
		s.items = make([]ITEM, 0)
		return
	}
	s.items = s.items[1:]
	return
}

func (s *Queue[ITEM]) Put(i ITEM) *Queue[ITEM] {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.items = append(s.items, i)
	return s
}

func (s *Queue[ITEM]) Empty() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.items) == 0
}

func NewQueue[ITEM any]() *Queue[ITEM] {
	return &Queue[ITEM]{}
}

type segmentBatch struct {
	Batch []analytics.Track `json:"batch"`
}

const (
	interval      = 10 * time.Millisecond
	version       = "100.0.0"
	applicationId = "foo"
	writeKey      = "key"
)

var (
	module = fx.Options(
		fx.NopLogger,
		NewHeartbeatModule(version, writeKey, interval),
		fx.Replace(func() AppIdProvider {
			return AppIdProviderFn(func(ctx context.Context) (string, error) {
				return applicationId, nil
			})
		}),
		fx.Supply(fx.Annotate(PropertiesEnricherFn(func(ctx context.Context, p analytics.Properties) error {
			p.Set("foo", "bar")
			return nil
		}), fx.ResultTags(FXTagPropertiesEnrichers), fx.As(new(PropertiesEnricher)))),
	)
)

func EventuallyQueueNotEmpty[ITEM any](t *testing.T, queue *Queue[ITEM]) {
	require.Eventually(t, func() bool {
		return !queue.Empty()
	}, 10*interval, interval)
}

var emptyHttpResponse = &http.Response{
	Body:       io.NopCloser(bytes.NewReader([]byte{})),
	StatusCode: http.StatusOK,
}

func newApp(module fx.Option, t transport) *fx.App {
	return fx.New(module, fx.Replace(analytics.Config{
		BatchSize: 1,
		Transport: t,
	}))
}

func withApp(t *testing.T, app *fx.App, fn func(t *testing.T)) {
	require.NoError(t, app.Start(context.Background()))
	defer func() {
		require.NoError(t, app.Stop(context.Background()))
	}()
	fn(t)
}

func TestSegment(t *testing.T) {

	t.Run("Nominal case", func(t *testing.T) {
		queue := NewQueue[*http.Request]()
		app := newApp(module, func(request *http.Request) (*http.Response, error) {
			queue.Put(request)
			return emptyHttpResponse, nil
		})
		withApp(t, app, func(t *testing.T) {
			for i := 0; i < 10; i++ {
				EventuallyQueueNotEmpty(t, queue)
				request, ok := queue.Get()
				require.True(t, ok)

				username, password, ok := request.BasicAuth()
				require.True(t, ok)
				require.Equal(t, writeKey, username)
				require.Empty(t, password)

				batch := &segmentBatch{}
				require.NoError(t, json.NewDecoder(request.Body).Decode(batch))
				require.Len(t, batch.Batch, 1)

				track := batch.Batch[0]
				require.Equal(t, ApplicationStats, track.Event)
				require.Equal(t, "bar", track.Properties["foo"])
			}
		})
	})
	t.Run("With error on the backend", func(t *testing.T) {
		firstCall := true

		queue := NewQueue[*http.Request]()
		app := newApp(module, func(request *http.Request) (*http.Response, error) {
			if firstCall {
				firstCall = false
				return nil, errors.New("error on the first try")
			}
			queue.Put(request)
			return emptyHttpResponse, nil
		})
		withApp(t, app, func(t *testing.T) {
			EventuallyQueueNotEmpty(t, queue)

			_, ok := queue.Get()
			require.True(t, ok)
		})
	})
}
