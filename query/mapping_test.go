package query

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultComparisonOperatorsMapping(t *testing.T) {
	t.Parallel()

	require.Equal(t, "=", DefaultComparisonOperatorsMapping["$match"])
	require.Equal(t, ">=", DefaultComparisonOperatorsMapping["$gte"])
	require.Equal(t, "<=", DefaultComparisonOperatorsMapping["$lte"])
	require.Equal(t, ">", DefaultComparisonOperatorsMapping["$gt"])
	require.Equal(t, "<", DefaultComparisonOperatorsMapping["$lt"])
	require.Equal(t, "LIKE", DefaultComparisonOperatorsMapping["$like"])
	require.Equal(t, "IS NOT NULL", DefaultComparisonOperatorsMapping["$exists"])
}

func TestDefaultMappingContext(t *testing.T) {
	t.Parallel()

	ctx := DefaultMappingContext()

	query, args, err := ctx.BuildMatcher("field", "$match", "value")
	require.NoError(t, err)
	require.Equal(t, "field = ?", query)
	require.Equal(t, []any{"value"}, args)

	query, args, err = ctx.BuildMatcher("field", "$gte", 10)
	require.NoError(t, err)
	require.Equal(t, "field >= ?", query)
	require.Equal(t, []any{10}, args)

	query, args, err = ctx.BuildMatcher("field", "$lte", 20)
	require.NoError(t, err)
	require.Equal(t, "field <= ?", query)
	require.Equal(t, []any{20}, args)

	query, args, err = ctx.BuildMatcher("field", "$gt", 5)
	require.NoError(t, err)
	require.Equal(t, "field > ?", query)
	require.Equal(t, []any{5}, args)

	query, args, err = ctx.BuildMatcher("field", "$lt", 15)
	require.NoError(t, err)
	require.Equal(t, "field < ?", query)
	require.Equal(t, []any{15}, args)

	query, args, err = ctx.BuildMatcher("field", "$like", "%test%")
	require.NoError(t, err)
	require.Equal(t, "field LIKE ?", query)
	require.Equal(t, []any{"%test%"}, args)

	query, args, err = ctx.BuildMatcher("field", "$exists", true)
	require.NoError(t, err)
	require.Equal(t, "field IS NOT NULL", query)
	require.Empty(t, args)

	query, args, err = ctx.BuildMatcher("field", "$exists", false)
	require.NoError(t, err)
	require.Equal(t, "field IS NULL", query)
	require.Empty(t, args)

	query, args, err = ctx.BuildMatcher("field", "$unknown", "value")
	require.Error(t, err)
	require.Empty(t, query)
	require.Empty(t, args)
}
