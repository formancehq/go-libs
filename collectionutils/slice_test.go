package collectionutils_test

import (
	"testing"

	"github.com/formancehq/go-libs/v2/collectionutils"
	"github.com/stretchr/testify/assert"
)

func TestMap(t *testing.T) {
	testCases := []struct {
		name     string
		input    []int
		mapper   func(int) string
		expected []string
	}{
		{
			name:     "empty slice",
			input:    []int{},
			mapper:   func(i int) string { return string(rune(i + '0')) },
			expected: []string{},
		},
		{
			name:     "single element",
			input:    []int{1},
			mapper:   func(i int) string { return string(rune(i + '0')) },
			expected: []string{"1"},
		},
		{
			name:     "multiple elements",
			input:    []int{1, 2, 3, 4, 5},
			mapper:   func(i int) string { return string(rune(i + '0')) },
			expected: []string{"1", "2", "3", "4", "5"},
		},
		{
			name:     "complex mapping",
			input:    []int{1, 2, 3},
			mapper:   func(i int) string { return string(rune(i+'0')) + "-mapped" },
			expected: []string{"1-mapped", "2-mapped", "3-mapped"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := collectionutils.Map(tc.input, tc.mapper)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestCopyMap(t *testing.T) {
	testCases := []struct {
		name  string
		input map[string]int
	}{
		{
			name:  "empty map",
			input: map[string]int{},
		},
		{
			name:  "map with one key",
			input: map[string]int{"one": 1},
		},
		{
			name:  "map with multiple keys",
			input: map[string]int{"one": 1, "two": 2, "three": 3},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := collectionutils.CopyMap(tc.input)

			// Verify the copy is equal to the original
			assert.Equal(t, tc.input, result)

			// Verify that modifying the copy doesn't affect the original
			if len(result) > 0 {
				for k := range result {
					result[k] = 999
					break
				}

				// Find the key we modified
				var modifiedKey string
				for k, v := range result {
					if v == 999 {
						modifiedKey = k
						break
					}
				}

				// Verify the original is unchanged
				if modifiedKey != "" {
					assert.NotEqual(t, 999, tc.input[modifiedKey])
				}
			}
		})
	}
}

func TestFilter(t *testing.T) {
	testCases := []struct {
		name     string
		input    []int
		filter   func(int) bool
		expected []int
	}{
		{
			name:     "empty slice",
			input:    []int{},
			filter:   func(i int) bool { return i > 2 },
			expected: []int{},
		},
		{
			name:     "no matches",
			input:    []int{1, 2},
			filter:   func(i int) bool { return i > 2 },
			expected: []int{},
		},
		{
			name:     "all match",
			input:    []int{3, 4, 5},
			filter:   func(i int) bool { return i > 2 },
			expected: []int{3, 4, 5},
		},
		{
			name:     "some match",
			input:    []int{1, 2, 3, 4, 5},
			filter:   func(i int) bool { return i > 2 },
			expected: []int{3, 4, 5},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := collectionutils.Filter(tc.input, tc.filter)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestReduce(t *testing.T) {
	testCases := []struct {
		name     string
		input    []int
		reducer  func(int, int) int
		initial  int
		expected int
	}{
		{
			name:     "empty slice",
			input:    []int{},
			reducer:  func(acc, i int) int { return acc + i },
			initial:  0,
			expected: 0,
		},
		{
			name:     "sum",
			input:    []int{1, 2, 3, 4, 5},
			reducer:  func(acc, i int) int { return acc + i },
			initial:  0,
			expected: 15,
		},
		{
			name:     "product",
			input:    []int{1, 2, 3, 4, 5},
			reducer:  func(acc, i int) int { return acc * i },
			initial:  1,
			expected: 120,
		},
		{
			name:  "max",
			input: []int{1, 5, 3, 9, 2},
			reducer: func(acc, i int) int {
				if i > acc {
					return i
				} else {
					return acc
				}
			},
			initial:  0,
			expected: 9,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := collectionutils.Reduce(tc.input, tc.reducer, tc.initial)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestFlatten(t *testing.T) {
	testCases := []struct {
		name     string
		input    [][]int
		expected []int
	}{
		{
			name:     "empty slice",
			input:    [][]int{},
			expected: []int{},
		},
		{
			name:     "empty nested slices",
			input:    [][]int{{}, {}, {}},
			expected: []int{},
		},
		{
			name:     "single nested slice",
			input:    [][]int{{1, 2, 3}},
			expected: []int{1, 2, 3},
		},
		{
			name:     "multiple nested slices",
			input:    [][]int{{1, 2}, {3, 4}, {5, 6}},
			expected: []int{1, 2, 3, 4, 5, 6},
		},
		{
			name:     "mixed empty and non-empty slices",
			input:    [][]int{{}, {1, 2}, {}, {3, 4}, {}},
			expected: []int{1, 2, 3, 4},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := collectionutils.Flatten(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestFirst(t *testing.T) {
	testCases := []struct {
		name     string
		input    []int
		filter   func(int) bool
		expected int
	}{
		{
			name:     "empty slice",
			input:    []int{},
			filter:   func(i int) bool { return i > 2 },
			expected: 0, // zero value for int
		},
		{
			name:     "no matches",
			input:    []int{1, 2},
			filter:   func(i int) bool { return i > 2 },
			expected: 0, // zero value for int
		},
		{
			name:     "one match",
			input:    []int{1, 3, 2},
			filter:   func(i int) bool { return i > 2 },
			expected: 3,
		},
		{
			name:     "multiple matches (returns first)",
			input:    []int{1, 3, 4, 5},
			filter:   func(i int) bool { return i > 2 },
			expected: 3,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := collectionutils.First(tc.input, tc.filter)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestFilterEq(t *testing.T) {
	testCases := []struct {
		name     string
		value    interface{}
		testVal  interface{}
		expected bool
	}{
		{
			name:     "equal ints",
			value:    42,
			testVal:  42,
			expected: true,
		},
		{
			name:     "different ints",
			value:    42,
			testVal:  43,
			expected: false,
		},
		{
			name:     "equal strings",
			value:    "test",
			testVal:  "test",
			expected: true,
		},
		{
			name:     "different strings",
			value:    "test",
			testVal:  "other",
			expected: false,
		},
		{
			name:     "equal structs",
			value:    struct{ Name string }{"John"},
			testVal:  struct{ Name string }{"John"},
			expected: true,
		},
		{
			name:     "different structs",
			value:    struct{ Name string }{"John"},
			testVal:  struct{ Name string }{"Jane"},
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			filter := collectionutils.FilterEq(tc.value)
			result := filter(tc.testVal)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestFilterNot(t *testing.T) {
	testCases := []struct {
		name     string
		filter   func(int) bool
		testVal  int
		expected bool
	}{
		{
			name:     "true becomes false",
			filter:   func(i int) bool { return i > 5 },
			testVal:  10,
			expected: false,
		},
		{
			name:     "false becomes true",
			filter:   func(i int) bool { return i > 5 },
			testVal:  3,
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			notFilter := collectionutils.FilterNot(tc.filter)
			result := notFilter(tc.testVal)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestContains(t *testing.T) {
	testCases := []struct {
		name     string
		slice    []interface{}
		value    interface{}
		expected bool
	}{
		{
			name:     "empty slice",
			slice:    []interface{}{},
			value:    42,
			expected: false,
		},
		{
			name:     "contains value",
			slice:    []interface{}{1, "test", 42, true},
			value:    42,
			expected: true,
		},
		{
			name:     "doesn't contain value",
			slice:    []interface{}{1, "test", true},
			value:    42,
			expected: false,
		},
		{
			name:     "contains struct",
			slice:    []interface{}{struct{ Name string }{"John"}, struct{ Name string }{"Jane"}},
			value:    struct{ Name string }{"John"},
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := collectionutils.Contains(tc.slice, tc.value)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestSet(t *testing.T) {
	t.Run("NewSet", func(t *testing.T) {
		set := collectionutils.NewSet[string]()
		assert.NotNil(t, set)
		assert.Equal(t, 0, len(set))
	})

	t.Run("Put", func(t *testing.T) {
		set := collectionutils.NewSet[string]()

		// Add single element
		set.Put("one")
		assert.Equal(t, 1, len(set))
		assert.True(t, set.Contains("one"))

		// Add multiple elements
		set.Put("two", "three")
		assert.Equal(t, 3, len(set))
		assert.True(t, set.Contains("two"))
		assert.True(t, set.Contains("three"))

		// Add duplicate element
		set.Put("one")
		assert.Equal(t, 3, len(set))
	})

	t.Run("Contains", func(t *testing.T) {
		set := collectionutils.NewSet[string]()
		set.Put("one", "two")

		assert.True(t, set.Contains("one"))
		assert.True(t, set.Contains("two"))
		assert.False(t, set.Contains("three"))
	})

	t.Run("ToSlice", func(t *testing.T) {
		set := collectionutils.NewSet[string]()
		set.Put("one", "two", "three")

		slice := set.ToSlice()
		assert.Equal(t, 3, len(slice))
		assert.Contains(t, slice, "one")
		assert.Contains(t, slice, "two")
		assert.Contains(t, slice, "three")
	})

	t.Run("Remove", func(t *testing.T) {
		set := collectionutils.NewSet[string]()
		set.Put("one", "two", "three")

		set.Remove("two")
		assert.Equal(t, 2, len(set))
		assert.True(t, set.Contains("one"))
		assert.False(t, set.Contains("two"))
		assert.True(t, set.Contains("three"))

		// Remove non-existent element
		set.Remove("four")
		assert.Equal(t, 2, len(set))
	})
}

func TestPrepend(t *testing.T) {
	testCases := []struct {
		name     string
		slice    []int
		value    int
		expected []int
	}{
		{
			name:     "empty slice",
			slice:    []int{},
			value:    1,
			expected: []int{1},
		},
		{
			name:     "non-empty slice",
			slice:    []int{2, 3, 4},
			value:    1,
			expected: []int{1, 2, 3, 4},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := collectionutils.Prepend(tc.slice, tc.value)
			assert.Equal(t, tc.expected, result)
		})
	}
}
