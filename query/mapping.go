package query

import (
	"fmt"
)

var DefaultComparisonOperatorsMapping = map[string]string{
	"$match":  "=",
	"$gte":    ">=",
	"$gt":     ">",
	"$lte":    "<=",
	"$lt":     "<",
	"$like":   "LIKE",
	"$exists": "IS NOT NULL",
}

func DefaultMappingContext() Context {
	return ContextFn(func(key, operator string, value any) (string, []any, error) {
		sqlOperator, ok := DefaultComparisonOperatorsMapping[operator]
		if !ok {
			return "", nil, fmt.Errorf("unknown operator: %s", operator)
		}

		if operator == "$exists" {
			if value == true {
				return key + " IS NOT NULL", nil, nil
			}
			return key + " IS NULL", nil, nil
		}

		return key + " " + sqlOperator + " ?", []any{value}, nil
	})
}
