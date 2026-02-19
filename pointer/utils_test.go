package pointer_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/formancehq/go-libs/v4/pointer"
)

func TestFor(t *testing.T) {
	t.Parallel()
	t.Run("string", func(t *testing.T) {
		t.Parallel()
		value := "test"
		ptr := pointer.For(value)

		require.NotNil(t, ptr)
		require.Equal(t, value, *ptr)

		// Verify that changing the original value doesn't affect the pointer
		value = "changed" //nolint:ineffassign
		require.Equal(t, "test", *ptr)
	})

	t.Run("int", func(t *testing.T) {
		t.Parallel()
		value := 42
		ptr := pointer.For(value)

		require.NotNil(t, ptr)
		require.Equal(t, value, *ptr)

		// Verify that changing the original value doesn't affect the pointer
		value = 100 //nolint:ineffassign
		require.Equal(t, 42, *ptr)
	})

	t.Run("bool", func(t *testing.T) {
		t.Parallel()
		value := true
		ptr := pointer.For(value)

		require.NotNil(t, ptr)
		require.Equal(t, value, *ptr)

		// Verify that changing the original value doesn't affect the pointer
		value = false //nolint:ineffassign
		require.Equal(t, true, *ptr)
	})

	t.Run("float", func(t *testing.T) {
		t.Parallel()
		value := 3.14
		ptr := pointer.For(value)

		require.NotNil(t, ptr)
		require.Equal(t, value, *ptr)

		// Verify that changing the original value doesn't affect the pointer
		value = 2.71 //nolint:ineffassign
		require.Equal(t, 3.14, *ptr)
	})

	t.Run("struct", func(t *testing.T) {
		t.Parallel()
		type TestStruct struct {
			Name string
			Age  int
		}

		value := TestStruct{Name: "John", Age: 30}
		ptr := pointer.For(value)

		require.NotNil(t, ptr)
		require.Equal(t, value, *ptr)

		// Verify that changing the original value doesn't affect the pointer
		value.Name = "Jane"
		value.Age = 25 //nolint:ineffassign
		require.Equal(t, TestStruct{Name: "John", Age: 30}, *ptr)
	})

	t.Run("slice", func(t *testing.T) {
		t.Parallel()
		value := []int{1, 2, 3}
		ptr := pointer.For(value)

		require.NotNil(t, ptr)
		require.Equal(t, value, *ptr)

		// Verify that changing the original value doesn't affect the pointer
		value = append(value, 4) //nolint:ineffassign
		require.Equal(t, []int{1, 2, 3}, *ptr)
	})

	t.Run("map", func(t *testing.T) {
		t.Parallel()
		// Maps are reference types in Go, so we need to create a copy
		// to test that the pointer points to a different map
		value := map[string]int{"one": 1, "two": 2}
		ptr := pointer.For(value)

		require.NotNil(t, ptr)
		require.Equal(t, value, *ptr)

		// Create a copy of the original map for comparison
		originalMap := map[string]int{"one": 1, "two": 2}

		// Modify the original map
		value["three"] = 3

		// The pointer should still point to a map with the original values
		// but since maps are reference types, the pointer's map is also modified
		require.Equal(t, value, *ptr)

		// Verify that the pointer's map is different from the original map (before modification)
		require.NotEqual(t, originalMap, *ptr)
	})

	t.Run("zero values", func(t *testing.T) {
		t.Parallel()
		// String
		strPtr := pointer.For("")
		require.NotNil(t, strPtr)
		require.Equal(t, "", *strPtr)

		// Int
		intPtr := pointer.For(0)
		require.NotNil(t, intPtr)
		require.Equal(t, 0, *intPtr)

		// Bool
		boolPtr := pointer.For(false)
		require.NotNil(t, boolPtr)
		require.Equal(t, false, *boolPtr)

		// Struct
		type EmptyStruct struct{}
		structPtr := pointer.For(EmptyStruct{})
		require.NotNil(t, structPtr)
		require.Equal(t, EmptyStruct{}, *structPtr)

		// Slice
		slicePtr := pointer.For([]int{})
		require.NotNil(t, slicePtr)
		require.Equal(t, []int{}, *slicePtr)

		// Map
		mapPtr := pointer.For(map[string]int{})
		require.NotNil(t, mapPtr)
		require.Equal(t, map[string]int{}, *mapPtr)
	})

	t.Run("nil values", func(t *testing.T) {
		t.Parallel()
		// Nil slice
		var nilSlice []int
		slicePtr := pointer.For(nilSlice)
		require.NotNil(t, slicePtr)
		require.Nil(t, *slicePtr)

		// Nil map
		var nilMap map[string]int
		mapPtr := pointer.For(nilMap)
		require.NotNil(t, mapPtr)
		require.Nil(t, *mapPtr)

		// Nil interface
		var nilInterface interface{}
		interfacePtr := pointer.For(nilInterface)
		require.NotNil(t, interfacePtr)
		require.Nil(t, *interfacePtr)
	})
}
