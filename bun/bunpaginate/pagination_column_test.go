package bunpaginate_test

import (
	"context"
	"math/big"
	"testing"

	"github.com/formancehq/go-libs/v3/bun/bundebug"
	"github.com/uptrace/bun"

	"github.com/formancehq/go-libs/v3/bun/bunconnect"
	"github.com/formancehq/go-libs/v3/bun/bunpaginate"
	"github.com/formancehq/go-libs/v3/logging"

	"github.com/stretchr/testify/require"
)

func TestColumnPagination(t *testing.T) {
	t.Parallel()

	hooks := make([]bun.QueryHook, 0)
	if testing.Verbose() {
		hooks = append(hooks, bundebug.NewQueryHook())
	}

	database := srv.NewDatabase(t)
	db, err := bunconnect.OpenSQLDB(logging.TestingContext(), bunconnect.ConnectionOptions{
		DatabaseSourceName: database.ConnString(),
	}, hooks...)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = db.Close()
	})

	_, err = db.Exec(`
		CREATE TABLE "models" (id int, pair boolean);
	`)
	require.NoError(t, err)

	type model struct {
		ID   *bunpaginate.BigInt `bun:"id,type:numeric"`
		Pair bool                `bun:"pair"`
	}

	models := make([]model, 0)
	for i := 0; i < 100; i++ {
		models = append(models, model{
			ID:   (*bunpaginate.BigInt)(big.NewInt(int64(i))),
			Pair: i%2 == 0,
		})
	}

	_, err = db.NewInsert().
		Model(&models).
		Exec(context.Background())
	require.NoError(t, err)

	type testCase struct {
		name                  string
		query                 bunpaginate.ColumnPaginatedQuery[bool]
		expectedNext          *bunpaginate.ColumnPaginatedQuery[bool]
		expectedPrevious      *bunpaginate.ColumnPaginatedQuery[bool]
		expectedNumberOfItems int64
	}
	testCases := []testCase{
		{
			name: "asc first page",
			query: bunpaginate.ColumnPaginatedQuery[bool]{
				PageSize: 10,
				Column:   "id",
				Order:    bunpaginate.OrderAsc,
			},
			expectedNext: &bunpaginate.ColumnPaginatedQuery[bool]{
				PageSize:     10,
				Column:       "id",
				PaginationID: big.NewInt(int64(10)),
				Order:        bunpaginate.OrderAsc,
				Bottom:       big.NewInt(int64(0)),
			},
			expectedNumberOfItems: 10,
		},
		{
			name: "asc second page using next cursor",
			query: bunpaginate.ColumnPaginatedQuery[bool]{
				PageSize:     10,
				Column:       "id",
				PaginationID: big.NewInt(int64(10)),
				Order:        bunpaginate.OrderAsc,
				Bottom:       big.NewInt(int64(0)),
			},
			expectedPrevious: &bunpaginate.ColumnPaginatedQuery[bool]{
				PageSize:     10,
				Column:       "id",
				Order:        bunpaginate.OrderAsc,
				Bottom:       big.NewInt(int64(0)),
				PaginationID: big.NewInt(int64(10)),
				Reverse:      true,
			},
			expectedNext: &bunpaginate.ColumnPaginatedQuery[bool]{
				PageSize:     10,
				Column:       "id",
				PaginationID: big.NewInt(int64(20)),
				Order:        bunpaginate.OrderAsc,
				Bottom:       big.NewInt(int64(0)),
			},
			expectedNumberOfItems: 10,
		},
		{
			name: "asc last page using next cursor",
			query: bunpaginate.ColumnPaginatedQuery[bool]{
				PageSize:     10,
				Column:       "id",
				PaginationID: big.NewInt(int64(90)),
				Order:        bunpaginate.OrderAsc,
				Bottom:       big.NewInt(int64(0)),
			},
			expectedPrevious: &bunpaginate.ColumnPaginatedQuery[bool]{
				PageSize:     10,
				Column:       "id",
				Order:        bunpaginate.OrderAsc,
				PaginationID: big.NewInt(int64(90)),
				Bottom:       big.NewInt(int64(0)),
				Reverse:      true,
			},
			expectedNumberOfItems: 10,
		},
		{
			name: "desc first page",
			query: bunpaginate.ColumnPaginatedQuery[bool]{
				PageSize: 10,
				Column:   "id",
				Order:    bunpaginate.OrderDesc,
			},
			expectedNext: &bunpaginate.ColumnPaginatedQuery[bool]{
				PageSize:     10,
				Bottom:       big.NewInt(int64(99)),
				Column:       "id",
				PaginationID: big.NewInt(int64(89)),
				Order:        bunpaginate.OrderDesc,
			},
			expectedNumberOfItems: 10,
		},
		{
			name: "desc second page using next cursor",
			query: bunpaginate.ColumnPaginatedQuery[bool]{
				PageSize:     10,
				Bottom:       big.NewInt(int64(99)),
				Column:       "id",
				PaginationID: big.NewInt(int64(89)),
				Order:        bunpaginate.OrderDesc,
			},
			expectedPrevious: &bunpaginate.ColumnPaginatedQuery[bool]{
				PageSize:     10,
				Bottom:       big.NewInt(int64(99)),
				Column:       "id",
				PaginationID: big.NewInt(int64(89)),
				Order:        bunpaginate.OrderDesc,
				Reverse:      true,
			},
			expectedNext: &bunpaginate.ColumnPaginatedQuery[bool]{
				PageSize:     10,
				Bottom:       big.NewInt(int64(99)),
				Column:       "id",
				PaginationID: big.NewInt(int64(79)),
				Order:        bunpaginate.OrderDesc,
			},
			expectedNumberOfItems: 10,
		},
		{
			name: "desc last page using next cursor",
			query: bunpaginate.ColumnPaginatedQuery[bool]{
				PageSize:     10,
				Bottom:       big.NewInt(int64(99)),
				Column:       "id",
				PaginationID: big.NewInt(int64(9)),
				Order:        bunpaginate.OrderDesc,
			},
			expectedPrevious: &bunpaginate.ColumnPaginatedQuery[bool]{
				PageSize:     10,
				Bottom:       big.NewInt(int64(99)),
				Column:       "id",
				PaginationID: big.NewInt(int64(9)),
				Order:        bunpaginate.OrderDesc,
				Reverse:      true,
			},
			expectedNumberOfItems: 10,
		},
		{
			name: "asc first page using previous cursor",
			query: bunpaginate.ColumnPaginatedQuery[bool]{
				PageSize:     10,
				Bottom:       big.NewInt(int64(0)),
				Column:       "id",
				PaginationID: big.NewInt(int64(10)),
				Order:        bunpaginate.OrderAsc,
				Reverse:      true,
			},
			expectedNext: &bunpaginate.ColumnPaginatedQuery[bool]{
				PageSize:     10,
				Bottom:       big.NewInt(int64(0)),
				Column:       "id",
				PaginationID: big.NewInt(int64(10)),
				Order:        bunpaginate.OrderAsc,
			},
			expectedNumberOfItems: 10,
		},
		{
			name: "desc first page using previous cursor",
			query: bunpaginate.ColumnPaginatedQuery[bool]{
				PageSize:     10,
				Bottom:       big.NewInt(int64(99)),
				Column:       "id",
				PaginationID: big.NewInt(int64(89)),
				Order:        bunpaginate.OrderDesc,
				Reverse:      true,
			},
			expectedNext: &bunpaginate.ColumnPaginatedQuery[bool]{
				PageSize:     10,
				Bottom:       big.NewInt(int64(99)),
				Column:       "id",
				PaginationID: big.NewInt(int64(89)),
				Order:        bunpaginate.OrderDesc,
			},
			expectedNumberOfItems: 10,
		},
		{
			name: "asc first page with filter",
			query: bunpaginate.ColumnPaginatedQuery[bool]{
				PageSize: 10,
				Column:   "id",
				Order:    bunpaginate.OrderAsc,
				Options:  true,
			},
			expectedNext: &bunpaginate.ColumnPaginatedQuery[bool]{
				PageSize:     10,
				Column:       "id",
				PaginationID: big.NewInt(int64(20)),
				Order:        bunpaginate.OrderAsc,
				Options:      true,
				Bottom:       big.NewInt(int64(0)),
			},
			expectedNumberOfItems: 10,
		},
		{
			name: "asc second page with filter",
			query: bunpaginate.ColumnPaginatedQuery[bool]{
				PageSize:     10,
				Column:       "id",
				PaginationID: big.NewInt(int64(20)),
				Order:        bunpaginate.OrderAsc,
				Options:      true,
				Bottom:       big.NewInt(int64(0)),
			},
			expectedNext: &bunpaginate.ColumnPaginatedQuery[bool]{
				PageSize:     10,
				Column:       "id",
				PaginationID: big.NewInt(int64(40)),
				Order:        bunpaginate.OrderAsc,
				Options:      true,
				Bottom:       big.NewInt(int64(0)),
			},
			expectedPrevious: &bunpaginate.ColumnPaginatedQuery[bool]{
				PageSize:     10,
				Column:       "id",
				PaginationID: big.NewInt(int64(20)),
				Order:        bunpaginate.OrderAsc,
				Options:      true,
				Bottom:       big.NewInt(int64(0)),
				Reverse:      true,
			},
			expectedNumberOfItems: 10,
		},
		{
			name: "desc first page with filter",
			query: bunpaginate.ColumnPaginatedQuery[bool]{
				PageSize: 10,
				Column:   "id",
				Order:    bunpaginate.OrderDesc,
				Options:  true,
			},
			expectedNext: &bunpaginate.ColumnPaginatedQuery[bool]{
				PageSize:     10,
				Column:       "id",
				PaginationID: big.NewInt(int64(78)),
				Order:        bunpaginate.OrderDesc,
				Options:      true,
				Bottom:       big.NewInt(int64(98)),
			},
			expectedNumberOfItems: 10,
		},
		{
			name: "desc second page with filter",
			query: bunpaginate.ColumnPaginatedQuery[bool]{
				PageSize:     10,
				Column:       "id",
				PaginationID: big.NewInt(int64(78)),
				Order:        bunpaginate.OrderDesc,
				Options:      true,
				Bottom:       big.NewInt(int64(98)),
			},
			expectedNext: &bunpaginate.ColumnPaginatedQuery[bool]{
				PageSize:     10,
				Column:       "id",
				PaginationID: big.NewInt(int64(58)),
				Order:        bunpaginate.OrderDesc,
				Options:      true,
				Bottom:       big.NewInt(int64(98)),
			},
			expectedPrevious: &bunpaginate.ColumnPaginatedQuery[bool]{
				PageSize:     10,
				Column:       "id",
				PaginationID: big.NewInt(int64(78)),
				Order:        bunpaginate.OrderDesc,
				Options:      true,
				Bottom:       big.NewInt(int64(98)),
				Reverse:      true,
			},
			expectedNumberOfItems: 10,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			models := make([]model, 0)
			query := db.NewSelect().Model(&models).Column("id")
			if tc.query.Options {
				query = query.Where("pair = ?", true)
			}
			cursor, err := bunpaginate.UsingColumn[bool, model](context.Background(), query, tc.query)
			require.NoError(t, err)

			if tc.expectedNext == nil {
				require.Empty(t, cursor.Next)
			} else {
				require.NotEmpty(t, cursor.Next)

				q := bunpaginate.ColumnPaginatedQuery[bool]{}
				require.NoError(t, bunpaginate.UnmarshalCursor(cursor.Next, &q))
				require.EqualValues(t, *tc.expectedNext, q)
			}

			if tc.expectedPrevious == nil {
				require.Empty(t, cursor.Previous)
			} else {
				require.NotEmpty(t, cursor.Previous)

				q := bunpaginate.ColumnPaginatedQuery[bool]{}
				require.NoError(t, bunpaginate.UnmarshalCursor(cursor.Previous, &q))
				require.EqualValues(t, *tc.expectedPrevious, q)
			}
		})
	}
}
