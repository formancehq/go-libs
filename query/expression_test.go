package query

import (
	"encoding/json"
	"fmt"
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
							"balance": 1000
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
	require.Equal(t, []any{
		"accounts::pending",
		float64(1000),
		"gold",
	}, args)
}

func TestParseJSON(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		input    string
		expected Builder
		err      bool
	}{
		{
			name:     "empty string",
			input:    "",
			expected: nil,
			err:      false,
		},
		{
			name:     "empty object",
			input:    "{}",
			expected: nil,
			err:      false,
		},
		{
			name:     "invalid json",
			input:    "{",
			expected: nil,
			err:      true,
		},
		{
			name:  "match operator",
			input: `{"$match": {"field": "value"}}`,
			expected: keyValue{
				operator: "$match",
				key:      "field",
				value:    "value",
			},
			err: false,
		},
		{
			name:  "and operator",
			input: `{"$and": [{"$match": {"field": "value"}}]}`,
			expected: set{
				operator: "and",
				items: []Builder{
					keyValue{
						operator: "$match",
						key:      "field",
						value:    "value",
					},
				},
			},
			err: false,
		},
		{
			name:  "or operator",
			input: `{"$or": [{"$match": {"field": "value"}}]}`,
			expected: set{
				operator: "or",
				items: []Builder{
					keyValue{
						operator: "$match",
						key:      "field",
						value:    "value",
					},
				},
			},
			err: false,
		},
		{
			name:  "not operator",
			input: `{"$not": {"$match": {"field": "value"}}}`,
			expected: not{
				expression: keyValue{
					operator: "$match",
					key:      "field",
					value:    "value",
				},
			},
			err: false,
		},
		{
			name:     "unknown operator",
			input:    `{"$unknown": {"field": "value"}}`,
			expected: nil,
			err:      true,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result, err := ParseJSON(tc.input)
			if tc.err {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				if tc.expected == nil {
					require.Nil(t, result)
				} else {
					// Convert to JSON for comparison since the Builder interface doesn't have an Equal method
					expectedJSON, err := json.Marshal(tc.expected)
					require.NoError(t, err)
					resultJSON, err := json.Marshal(result)
					require.NoError(t, err)
					require.JSONEq(t, string(expectedJSON), string(resultJSON))
				}
			}
		})
	}
}

func TestBuilderFunctions(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		builder  Builder
		expected string
		args     []any
	}{
		{
			name:     "Match",
			builder:  Match("field", "value"),
			expected: "field = ?",
			args:     []any{"value"},
		},
		{
			name:     "Lt",
			builder:  Lt("field", 10),
			expected: "field < ?",
			args:     []any{10},
		},
		{
			name:     "Lte",
			builder:  Lte("field", 10),
			expected: "field <= ?",
			args:     []any{10},
		},
		{
			name:     "Gt",
			builder:  Gt("field", 10),
			expected: "field > ?",
			args:     []any{10},
		},
		{
			name:     "Gte",
			builder:  Gte("field", 10),
			expected: "field >= ?",
			args:     []any{10},
		},
		{
			name:     "Exists",
			builder:  Exists("field", true),
			expected: "field  ?",
			args:     []any{true},
		},
		{
			name:     "Not",
			builder:  Not(Match("field", "value")),
			expected: "not (field = ?)",
			args:     []any{"value"},
		},
		{
			name:     "And with no items",
			builder:  And(),
			expected: "1 = 1",
			args:     []any(nil),
		},
		{
			name:     "Or with no items",
			builder:  Or(),
			expected: "1 = 1",
			args:     []any(nil),
		},
		{
			name:     "And with one item",
			builder:  And(Match("field", "value")),
			expected: "(field = ?)",
			args:     []any{"value"},
		},
		{
			name:     "Or with one item",
			builder:  Or(Match("field", "value")),
			expected: "(field = ?)",
			args:     []any{"value"},
		},
		{
			name:     "And with multiple items",
			builder:  And(Match("field1", "value1"), Match("field2", "value2")),
			expected: "(field1 = ?) and (field2 = ?)",
			args:     []any{"value1", "value2"},
		},
		{
			name:     "Or with multiple items",
			builder:  Or(Match("field1", "value1"), Match("field2", "value2")),
			expected: "(field1 = ?) or (field2 = ?)",
			args:     []any{"value1", "value2"},
		},
		{
			name:     "Complex expression",
			builder:  And(Match("field1", "value1"), Or(Match("field2", "value2"), Match("field3", "value3"))),
			expected: "(field1 = ?) and ((field2 = ?) or (field3 = ?))",
			args:     []any{"value1", "value2", "value3"},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := ContextFn(func(key, operator string, value any) (string, []any, error) {
				return fmt.Sprintf("%s %s ?", key, DefaultComparisonOperatorsMapping[operator]), []any{value}, nil
			})
			result, args, err := tc.builder.Build(ctx)
			require.NoError(t, err)
			require.Equal(t, tc.expected, result)
			require.Equal(t, tc.args, args)
		})
	}
}

func TestWalkFunction(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		builder  Builder
		expected map[string][]string
	}{
		{
			name:    "Match",
			builder: Match("field", "value"),
			expected: map[string][]string{
				"$match": {"field"},
			},
		},
		{
			name:    "Lt",
			builder: Lt("field", 10),
			expected: map[string][]string{
				"$lt": {"field"},
			},
		},
		{
			name:    "And with multiple items",
			builder: And(Match("field1", "value1"), Match("field2", "value2")),
			expected: map[string][]string{
				"$match": {"field1", "field2"},
			},
		},
		{
			name:    "Or with multiple items",
			builder: Or(Match("field1", "value1"), Match("field2", "value2")),
			expected: map[string][]string{
				"$match": {"field1", "field2"},
			},
		},
		{
			name:    "Not",
			builder: Not(Match("field", "value")),
			expected: map[string][]string{
				"$match": {"field"},
			},
		},
		{
			name:    "Complex expression",
			builder: And(Match("field1", "value1"), Or(Lt("field2", 10), Gte("field3", 20))),
			expected: map[string][]string{
				"$match": {"field1"},
				"$lt":    {"field2"},
				"$gte":   {"field3"},
			},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := make(map[string][]string)
			err := tc.builder.Walk(func(operator string, key string, value any) error {
				if _, ok := result[operator]; !ok {
					result[operator] = make([]string, 0)
				}
				result[operator] = append(result[operator], key)
				return nil
			})
			require.NoError(t, err)
			require.Equal(t, tc.expected, result)
		})
	}
}

func TestErrorHandling(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name    string
		builder Builder
		ctx     Context
		err     bool
	}{
		{
			name:    "Context returns error",
			builder: Match("field", "value"),
			ctx: ContextFn(func(key, operator string, value any) (string, []any, error) {
				return "", nil, fmt.Errorf("context error")
			}),
			err: true,
		},
		{
			name: "Walk returns error",
			builder: Match("field", "value"),
			ctx:     nil,
			err:     false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.ctx != nil {
				_, _, err := tc.builder.Build(tc.ctx)
				if tc.err {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
			}

			err := tc.builder.Walk(func(operator string, key string, value any) error {
				return fmt.Errorf("walk error")
			})
			require.Error(t, err)
		})
	}
}
