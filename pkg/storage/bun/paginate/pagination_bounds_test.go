package paginate_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	bunconnect "github.com/formancehq/go-libs/v5/pkg/storage/bun/connect"
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

func TestPaginationLargeBounds(t *testing.T) {
	t.Parallel()
	database := srv.NewDatabase(t)
	db, err := bunconnect.OpenSQLDB(t.Context(), database.ConnectionOptions())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	type model struct {
		ID *paginate.BigInt `bun:"id,type:numeric"`
	}
	selectRows := func() *bun.SelectQuery {
		return db.NewSelect().ModelTableExpr("(VALUES (1::numeric), (2::numeric), (3::numeric)) AS model(id)").Column("id").Order("id ASC")
	}
	size := uint64(min(math.MaxInt-1, math.MaxInt64-1))
	offsetCursor, err := paginate.UsingOffset[struct{}, model](t.Context(), selectRows(), paginate.OffsetPaginatedQuery[struct{}]{PageSize: size})
	require.NoError(t, err)
	require.Len(t, offsetCursor.Data, 3)
	require.False(t, offsetCursor.HasMore)
	require.Equal(t, int(size), offsetCursor.PageSize)
	columnCursor, err := paginate.UsingColumn[struct{}, model](t.Context(), selectRows(), paginate.ColumnPaginatedQuery[struct{}]{PageSize: size, Column: "id"})
	require.NoError(t, err)
	require.Len(t, columnCursor.Data, 3)
	require.False(t, columnCursor.HasMore)
	require.Equal(t, int(size), columnCursor.PageSize)

	for _, offset := range []uint64{1 << 32, math.MaxInt64} {
		cursor, err := paginate.UsingOffset[struct{}, model](t.Context(), selectRows(), paginate.OffsetPaginatedQuery[struct{}]{PageSize: 2, Offset: offset})
		require.NoError(t, err)
		require.Empty(t, cursor.Data)
		require.False(t, cursor.HasMore)
		previous := paginate.OffsetPaginatedQuery[struct{}]{}
		require.NoError(t, paginate.UnmarshalCursor(cursor.Previous, &previous))
		require.Equal(t, offset-2, previous.Offset)
	}
}
