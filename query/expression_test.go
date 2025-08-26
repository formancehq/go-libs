package query

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseExpression(t *testing.T) {
	t.Parallel()
	json := `{
	"$not": {
		"$and": [
			{
				"$match": {
					"account": "accounts::pending"
				}
			},
			{
				"$or": [
					{
						"$gte": {
							"balance": 288230376151711747
						}
					},
					{
						"$match": {
							"metadata[category]": "gold"
						}
					}
				]
			}
		]
	}
}`
	expr, err := ParseJSON(json)
	require.NoError(t, err)

	q, args, err := expr.Build(ContextFn(func(key, operator string, value any) (string, []any, error) {
		return fmt.Sprintf("%s %s ?", key, DefaultComparisonOperatorsMapping[operator]), []any{value}, nil
	}))
	require.NoError(t, err)
	require.Equal(t, "not ((account = ?) and ((balance >= ?) or (metadata[category] = ?)))", q)
	var expected_balance big.Int
	expected_balance.SetString("288230376151711747", 10)
	require.Equal(t, []any{
		"accounts::pending",
		&expected_balance,
		"gold",
	}, args)
}
