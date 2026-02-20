package api

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/formancehq/go-libs/v4/bun/bunpaginate"
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
