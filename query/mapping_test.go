package query_test

import (
	"testing"

	"github.com/formancehq/go-libs/v2/query"
	"github.com/stretchr/testify/require"
)

func TestDefaultComparisonOperatorsMapping(t *testing.T) {
	t.Parallel()

	// Verify the default mapping contains expected operators
	require.Equal(t, "=", query.DefaultComparisonOperatorsMapping["$match"])
	require.Equal(t, ">=", query.DefaultComparisonOperatorsMapping["$gte"])
	require.Equal(t, ">", query.DefaultComparisonOperatorsMapping["$gt"])
	require.Equal(t, "<=", query.DefaultComparisonOperatorsMapping["$lte"])
	require.Equal(t, "<", query.DefaultComparisonOperatorsMapping["$lt"])

	// Verify the map has the expected number of entries
	require.Len(t, query.DefaultComparisonOperatorsMapping, 5)
}
