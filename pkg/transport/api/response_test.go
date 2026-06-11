package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	bunpaginate "github.com/formancehq/go-libs/v5/pkg/storage/bun/paginate"
)

func TestCursor(t *testing.T) {
	t.Parallel()
	c := bunpaginate.Cursor[int64]{
		Data: []int64{1, 2, 3},
	}
	by, err := json.Marshal(c)
	require.NoError(t, err)
	require.Equal(t, `{"hasMore":false,"data":[1,2,3]}`, string(by))

	c = bunpaginate.Cursor[int64]{
		Data:    []int64{1, 2, 3},
		HasMore: true,
	}
	by, err = json.Marshal(c)
	require.NoError(t, err)
	require.Equal(t,
		`{"hasMore":true,"data":[1,2,3]}`,
		string(by))
}

func TestFetchAllPaginated(t *testing.T) {
	t.Parallel()

	t.Run("multi-page success", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var rsp BaseResponse[int64]
			switch r.URL.Query().Get("cursor") {
			case "":
				rsp = BaseResponse[int64]{
					Cursor: &bunpaginate.Cursor[int64]{
						HasMore: true,
						Next:    "page2",
						Data:    []int64{1, 2},
					},
				}
			case "page2":
				rsp = BaseResponse[int64]{
					Cursor: &bunpaginate.Cursor[int64]{
						HasMore: false,
						Data:    []int64{3, 4},
					},
				}
			default:
				http.Error(w, "unexpected cursor", http.StatusBadRequest)
				return
			}
			require.NoError(t, json.NewEncoder(w).Encode(rsp))
		}))
		t.Cleanup(srv.Close)

		ret, err := FetchAllPaginated[int64](context.Background(), srv.Client(), srv.URL, url.Values{})
		require.NoError(t, err)
		require.Equal(t, []int64{1, 2, 3, 4}, ret)
	})

	t.Run("200 without cursor returns an error", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.NoError(t, json.NewEncoder(w).Encode(BaseResponse[int64]{}))
		}))
		t.Cleanup(srv.Close)

		ret, err := FetchAllPaginated[int64](context.Background(), srv.Client(), srv.URL, url.Values{})
		require.Error(t, err)
		require.ErrorContains(t, err, "missing cursor")
		require.Nil(t, ret)
	})

	t.Run("repeated next cursor returns an error instead of looping", func(t *testing.T) {
		t.Parallel()

		var calls int
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			require.NoError(t, json.NewEncoder(w).Encode(BaseResponse[int64]{
				Cursor: &bunpaginate.Cursor[int64]{
					HasMore: true,
					Next:    "same-token",
					Data:    []int64{1},
				},
			}))
		}))
		t.Cleanup(srv.Close)

		ret, err := FetchAllPaginated[int64](context.Background(), srv.Client(), srv.URL, url.Values{})
		require.Error(t, err)
		require.ErrorContains(t, err, "did not advance")
		require.Nil(t, ret)
		require.Equal(t, 2, calls)
	})

	t.Run("non-200 status code returns an error", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "internal error", http.StatusInternalServerError)
		}))
		t.Cleanup(srv.Close)

		ret, err := FetchAllPaginated[int64](context.Background(), srv.Client(), srv.URL, url.Values{})
		require.Error(t, err)
		require.ErrorContains(t, err, "unexpected status code 500")
		require.Nil(t, ret)
	})
}
