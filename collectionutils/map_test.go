package collectionutils_test

import (
	"testing"

	"github.com/formancehq/go-libs/v3/collectionutils"
	"github.com/stretchr/testify/require"
)

func TestKeys(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		input    map[string]int
		expected []string
	}{
		{
			name:     "empty map",
			input:    map[string]int{},
			expected: []string{},
		},
		{
			name:     "map with one key",
			input:    map[string]int{"one": 1},
			expected: []string{"one"},
		},
		{
			name:     "map with multiple keys",
			input:    map[string]int{"one": 1, "two": 2, "three": 3},
			expected: []string{"one", "two", "three"},
		},
		{
			name:     "nil map",
			input:    nil,
			expected: []string{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := collectionutils.Keys(tc.input)

			// Since map iteration order is not guaranteed, we need to check that all keys are present
			// rather than checking the exact order
			require.Equal(t, len(tc.expected), len(result))
			for _, key := range tc.expected {
				require.Contains(t, result, key)
			}
		})
	}
}

func TestConvertMap(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		input    map[string]int
		mapper   func(int) string
		expected map[string]string
	}{
		{
			name:     "empty map",
			input:    map[string]int{},
			mapper:   func(i int) string { return "value" + string(rune(i+'0')) },
			expected: map[string]string{},
		},
		{
			name:     "map with one key",
			input:    map[string]int{"one": 1},
			mapper:   func(i int) string { return "value" + string(rune(i+'0')) },
			expected: map[string]string{"one": "value1"},
		},
		{
			name:     "map with multiple keys",
			input:    map[string]int{"one": 1, "two": 2, "three": 3},
			mapper:   func(i int) string { return "value" + string(rune(i+'0')) },
			expected: map[string]string{"one": "value1", "two": "value2", "three": "value3"},
		},
		{
			name:     "nil map",
			input:    nil,
			mapper:   func(i int) string { return "value" + string(rune(i+'0')) },
			expected: map[string]string{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := collectionutils.ConvertMap(tc.input, tc.mapper)
			require.Equal(t, tc.expected, result)
		})
	}
}

func TestMergeMaps(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		map1     map[string]int
		map2     map[string]int
		expected map[string]int
	}{
		{
			name:     "both maps empty",
			map1:     map[string]int{},
			map2:     map[string]int{},
			expected: map[string]int{},
		},
		{
			name:     "first map empty",
			map1:     map[string]int{},
			map2:     map[string]int{"one": 1, "two": 2},
			expected: map[string]int{"one": 1, "two": 2},
		},
		{
			name:     "second map empty",
			map1:     map[string]int{"one": 1, "two": 2},
			map2:     map[string]int{},
			expected: map[string]int{"one": 1, "two": 2},
		},
		{
			name:     "both maps with unique keys",
			map1:     map[string]int{"one": 1, "two": 2},
			map2:     map[string]int{"three": 3, "four": 4},
			expected: map[string]int{"one": 1, "two": 2, "three": 3, "four": 4},
		},
		{
			name:     "maps with overlapping keys (second map overwrites)",
			map1:     map[string]int{"one": 1, "two": 2, "three": 3},
			map2:     map[string]int{"two": 22, "three": 33, "four": 4},
			expected: map[string]int{"one": 1, "two": 22, "three": 33, "four": 4},
		},
		{
			name:     "first map nil",
			map1:     nil,
			map2:     map[string]int{"one": 1, "two": 2},
			expected: map[string]int{"one": 1, "two": 2},
		},
		{
			name:     "second map nil",
			map1:     map[string]int{"one": 1, "two": 2},
			map2:     nil,
			expected: map[string]int{"one": 1, "two": 2},
		},
		{
			name:     "both maps nil",
			map1:     nil,
			map2:     nil,
			expected: map[string]int{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := collectionutils.MergeMaps(tc.map1, tc.map2)
			require.Equal(t, tc.expected, result)
		})
	}
}

func TestToAny(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		input    interface{}
		expected interface{}
	}{
		{
			name:     "string",
			input:    "test",
			expected: "test",
		},
		{
			name:     "int",
			input:    42,
			expected: 42,
		},
		{
			name:     "bool",
			input:    true,
			expected: true,
		},
		{
			name:     "struct",
			input:    struct{ Name string }{"John"},
			expected: struct{ Name string }{"John"},
		},
		{
			name:     "nil",
			input:    nil,
			expected: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := collectionutils.ToAny(tc.input)
			require.Equal(t, tc.expected, result)
		})
	}
}

func TestToPointer(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name  string
		input interface{}
	}{
		{
			name:  "string",
			input: "test",
		},
		{
			name:  "int",
			input: 42,
		},
		{
			name:  "bool",
			input: true,
		},
		{
			name:  "struct",
			input: struct{ Name string }{"John"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := collectionutils.ToPointer(tc.input)
			require.NotNil(t, result)
			require.Equal(t, tc.input, *result)
		})
	}
}

func TestToFmtString(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		input    interface{}
		expected string
	}{
		{
			name:     "string",
			input:    "test",
			expected: "test",
		},
		{
			name:     "int",
			input:    42,
			expected: "42",
		},
		{
			name:     "bool",
			input:    true,
			expected: "true",
		},
		{
			name:     "struct",
			input:    struct{ Name string }{"John"},
			expected: "{John}",
		},
		{
			name:     "nil",
			input:    nil,
			expected: "<nil>",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := collectionutils.ToFmtString(tc.input)
			require.Equal(t, tc.expected, result)
		})
	}
}
