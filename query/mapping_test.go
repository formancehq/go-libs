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
}
