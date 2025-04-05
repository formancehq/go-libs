package query

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuilderFunctions(t *testing.T) {
	t.Parallel()
	
	match := Match("field", "value")
	require.Equal(t, "$match", match.operator)
	require.Equal(t, "field", match.key)
	require.Equal(t, "value", match.value)
	
	like := Like("field", "%value%")
	require.Equal(t, "$like", like.operator)
	require.Equal(t, "field", like.key)
	require.Equal(t, "%value%", like.value)
	
	or := Or(match, like)
	require.Equal(t, "or", or.operator)
	require.Len(t, or.items, 2)
	
	and := And(match, like)
	require.Equal(t, "and", and.operator)
	require.Len(t, and.items, 2)
	
	lt := Lt("field", 10)
	require.Equal(t, "$lt", lt.operator)
	require.Equal(t, "field", lt.key)
	require.Equal(t, 10, lt.value)
	
	lte := Lte("field", 10)
	require.Equal(t, "$lte", lte.operator)
	require.Equal(t, "field", lte.key)
	require.Equal(t, 10, lte.value)
	
	gt := Gt("field", 10)
	require.Equal(t, "$gt", gt.operator)
	require.Equal(t, "field", gt.key)
	require.Equal(t, 10, gt.value)
	
	gte := Gte("field", 10)
	require.Equal(t, "$gte", gte.operator)
	require.Equal(t, "field", gte.key)
	require.Equal(t, 10, gte.value)
	
	exists := Exists("field", true)
	require.Equal(t, "$exists", exists.operator)
	require.Equal(t, "field", exists.key)
	require.Equal(t, true, exists.value)
	
	not := Not(match)
	require.Equal(t, match, not.expression)
}

func TestParseJSON_EmptyString(t *testing.T) {
	t.Parallel()
	
	expr, err := ParseJSON("")
	require.NoError(t, err)
	require.Nil(t, expr)
	
	expr, err = ParseJSON("{}")
	require.NoError(t, err)
	require.Nil(t, expr)
	
	expr, err = ParseJSON("invalid")
	require.Error(t, err)
	require.Nil(t, expr)
}

func TestSetBuild_EmptyItems(t *testing.T) {
	t.Parallel()
	
	s := set{
		operator: "and",
		items:    []Builder{},
	}
	
	query, args, err := s.Build(DefaultMappingContext())
	require.NoError(t, err)
	require.Equal(t, "1 = 1", query)
	require.Empty(t, args)
}

func TestKeyValueMarshalJSON(t *testing.T) {
	t.Parallel()
	
	kv := keyValue{
		operator: "$match",
		key:      "field",
		value:    "value",
	}
	
	data, err := kv.MarshalJSON()
	require.NoError(t, err)
	require.Contains(t, string(data), "$match")
	require.Contains(t, string(data), "field")
	require.Contains(t, string(data), "value")
}

func TestSetMarshalJSON(t *testing.T) {
	t.Parallel()
	
	s := set{
		operator: "and",
		items:    []Builder{Match("field", "value")},
	}
	
	data, err := s.MarshalJSON()
	require.NoError(t, err)
	require.Contains(t, string(data), "$and")
}

func TestNotMarshalJSON(t *testing.T) {
	t.Parallel()
	
	n := not{
		expression: Match("field", "value"),
	}
	
	data, err := n.MarshalJSON()
	require.NoError(t, err)
	require.Contains(t, string(data), "$not")
}
