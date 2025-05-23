package bunpaginate_test

import (
	"context"
	"testing"

	"github.com/formancehq/go-libs/v3/bun/bundebug"
	"github.com/uptrace/bun"

	"github.com/formancehq/go-libs/v3/bun/bunconnect"
	bunpaginate2 "github.com/formancehq/go-libs/v3/bun/bunpaginate"
	"github.com/formancehq/go-libs/v3/logging"

	"github.com/stretchr/testify/require"
)

func TestOffsetPagination(t *testing.T) {
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
		ID   uint64 `bun:"id"`
		Pair bool   `bun:"pair"`
	}

	models := make([]model, 0)
	for i := 0; i < 100; i++ {
		models = append(models, model{
			ID:   uint64(i),
			Pair: i%2 == 0,
		})
	}

	_, err = db.NewInsert().
		Model(&models).
		Exec(context.Background())
	require.NoError(t, err)

	type testCase struct {
		name                  string
		query                 bunpaginate2.OffsetPaginatedQuery[bool]
		expectedNext          *bunpaginate2.OffsetPaginatedQuery[bool]
		expectedPrevious      *bunpaginate2.OffsetPaginatedQuery[bool]
		expectedNumberOfItems uint64
	}
	testCases := []testCase{
		{
			name: "asc first page",
			query: bunpaginate2.OffsetPaginatedQuery[bool]{
				PageSize: 10,
				Order:    bunpaginate2.OrderAsc,
			},
			expectedNext: &bunpaginate2.OffsetPaginatedQuery[bool]{
				PageSize: 10,
				Offset:   10,
				Order:    bunpaginate2.OrderAsc,
			},
			expectedNumberOfItems: 10,
		},
		{
			name: "asc second page using next cursor",
			query: bunpaginate2.OffsetPaginatedQuery[bool]{
				PageSize: 10,
				Offset:   10,
				Order:    bunpaginate2.OrderAsc,
			},
			expectedPrevious: &bunpaginate2.OffsetPaginatedQuery[bool]{
				PageSize: 10,
				Order:    bunpaginate2.OrderAsc,
				Offset:   0,
			},
			expectedNext: &bunpaginate2.OffsetPaginatedQuery[bool]{
				PageSize: 10,
				Order:    bunpaginate2.OrderAsc,
				Offset:   20,
			},
			expectedNumberOfItems: 10,
		},
		{
			name: "asc last page using next cursor",
			query: bunpaginate2.OffsetPaginatedQuery[bool]{
				PageSize: 10,
				Offset:   90,
				Order:    bunpaginate2.OrderAsc,
			},
			expectedPrevious: &bunpaginate2.OffsetPaginatedQuery[bool]{
				PageSize: 10,
				Order:    bunpaginate2.OrderAsc,
				Offset:   80,
			},
			expectedNumberOfItems: 10,
		},
		{
			name: "asc last page partial",
			query: bunpaginate2.OffsetPaginatedQuery[bool]{
				PageSize: 10,
				Offset:   95,
				Order:    bunpaginate2.OrderAsc,
			},
			expectedPrevious: &bunpaginate2.OffsetPaginatedQuery[bool]{
				PageSize: 10,
				Order:    bunpaginate2.OrderAsc,
				Offset:   85,
			},
			expectedNumberOfItems: 10,
		},
		{
			name: "asc fist page partial",
			query: bunpaginate2.OffsetPaginatedQuery[bool]{
				PageSize: 10,
				Offset:   5,
				Order:    bunpaginate2.OrderAsc,
			},
			expectedPrevious: &bunpaginate2.OffsetPaginatedQuery[bool]{
				PageSize: 10,
				Order:    bunpaginate2.OrderAsc,
				Offset:   0,
			},
			expectedNext: &bunpaginate2.OffsetPaginatedQuery[bool]{
				PageSize: 10,
				Order:    bunpaginate2.OrderAsc,
				Offset:   15,
			},
			expectedNumberOfItems: 10,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			query := db.NewSelect().Model(&models).Column("id")
			if tc.query.Options {
				query = query.Where("pair = ?", true)
			}
			cursor, err := bunpaginate2.UsingOffset[bool, model](
				context.Background(),
				query,
				tc.query)
			require.NoError(t, err)

			if tc.expectedNext == nil {
				require.Empty(t, cursor.Next)
			} else {
				require.NotEmpty(t, cursor.Next)

				q := bunpaginate2.OffsetPaginatedQuery[bool]{}
				require.NoError(t, bunpaginate2.UnmarshalCursor(cursor.Next, &q))
				require.EqualValues(t, *tc.expectedNext, q)
			}

			if tc.expectedPrevious == nil {
				require.Empty(t, cursor.Previous)
			} else {
				require.NotEmpty(t, cursor.Previous)

				q := bunpaginate2.OffsetPaginatedQuery[bool]{}
				require.NoError(t, bunpaginate2.UnmarshalCursor(cursor.Previous, &q))
				require.EqualValues(t, *tc.expectedPrevious, q)
			}
		})
	}
}
