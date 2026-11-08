package paginate_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/formancehq/go-libs/v5/pkg/storage/bun/paginate"
)

func TestPaginationRejectsUnrepresentableBounds(t *testing.T) {
	t.Parallel()
	for _, size := range []uint64{uint64(math.MaxInt), math.MaxInt64, math.MaxUint64} {
		t.Run("page size", func(t *testing.T) {
			// A nil query ensures rejection happens before building or executing SQL.
			_, err := paginate.UsingOffset[struct{}, struct{}](t.Context(), nil, paginate.OffsetPaginatedQuery[struct{}]{PageSize: size})
			require.ErrorIs(t, err, paginate.ErrInvalidPageSize)
			_, err = paginate.UsingColumn[struct{}, struct{}](t.Context(), nil, paginate.ColumnPaginatedQuery[struct{}]{PageSize: size})
			require.ErrorIs(t, err, paginate.ErrInvalidPageSize)
		})
	}
	for _, offset := range []uint64{uint64(math.MaxInt64) + 1, math.MaxUint64} {
		_, err := paginate.UsingOffset[struct{}, struct{}](t.Context(), nil, paginate.OffsetPaginatedQuery[struct{}]{Offset: offset, PageSize: 2})
		require.ErrorContains(t, err, "offset")
	}
}
