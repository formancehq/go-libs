package pointer_test

import (
	"testing"

	"github.com/formancehq/go-libs/v2/pointer"
	"github.com/stretchr/testify/assert"
)

func TestFor(t *testing.T) {
	t.Run("string", func(t *testing.T) {
		value := "test"
		ptr := pointer.For(value)

		assert.NotNil(t, ptr)
		assert.Equal(t, value, *ptr)

		// Verify that changing the original value doesn't affect the pointer
		value = "changed"
		assert.Equal(t, "test", *ptr)
	})

	t.Run("int", func(t *testing.T) {
		value := 42
		ptr := pointer.For(value)

		assert.NotNil(t, ptr)
		assert.Equal(t, value, *ptr)

		// Verify that changing the original value doesn't affect the pointer
		value = 100
		assert.Equal(t, 42, *ptr)
	})

	t.Run("bool", func(t *testing.T) {
		value := true
		ptr := pointer.For(value)

		assert.NotNil(t, ptr)
		assert.Equal(t, value, *ptr)

		// Verify that changing the original value doesn't affect the pointer
		value = false
		assert.Equal(t, true, *ptr)
	})

	t.Run("float", func(t *testing.T) {
		value := 3.14
		ptr := pointer.For(value)

		assert.NotNil(t, ptr)
		assert.Equal(t, value, *ptr)

		// Verify that changing the original value doesn't affect the pointer
		value = 2.71
		assert.Equal(t, 3.14, *ptr)
	})

	t.Run("struct", func(t *testing.T) {
		type TestStruct struct {
			Name string
			Age  int
		}

		value := TestStruct{Name: "John", Age: 30}
		ptr := pointer.For(value)

		assert.NotNil(t, ptr)
		assert.Equal(t, value, *ptr)

		// Verify that changing the original value doesn't affect the pointer
		value.Name = "Jane"
		value.Age = 25
		assert.Equal(t, TestStruct{Name: "John", Age: 30}, *ptr)
	})

	t.Run("slice", func(t *testing.T) {
		value := []int{1, 2, 3}
		ptr := pointer.For(value)

		assert.NotNil(t, ptr)
		assert.Equal(t, value, *ptr)

		// Verify that changing the original value doesn't affect the pointer
		value = append(value, 4)
		assert.Equal(t, []int{1, 2, 3}, *ptr)
	})

	t.Run("map", func(t *testing.T) {
		// Maps are reference types in Go, so we need to create a copy
		// to test that the pointer points to a different map
		value := map[string]int{"one": 1, "two": 2}
		ptr := pointer.For(value)

		assert.NotNil(t, ptr)
		assert.Equal(t, value, *ptr)

		// Create a copy of the original map for comparison
		originalMap := map[string]int{"one": 1, "two": 2}

		// Modify the original map
		value["three"] = 3

		// The pointer should still point to a map with the original values
		// but since maps are reference types, the pointer's map is also modified
		assert.Equal(t, value, *ptr)

		// Verify that the pointer's map is different from the original map (before modification)
		assert.NotEqual(t, originalMap, *ptr)
	})

	t.Run("zero values", func(t *testing.T) {
		// String
		strPtr := pointer.For("")
		assert.NotNil(t, strPtr)
		assert.Equal(t, "", *strPtr)

		// Int
		intPtr := pointer.For(0)
		assert.NotNil(t, intPtr)
		assert.Equal(t, 0, *intPtr)

		// Bool
		boolPtr := pointer.For(false)
		assert.NotNil(t, boolPtr)
		assert.Equal(t, false, *boolPtr)

		// Struct
		type EmptyStruct struct{}
		structPtr := pointer.For(EmptyStruct{})
		assert.NotNil(t, structPtr)
		assert.Equal(t, EmptyStruct{}, *structPtr)

		// Slice
		slicePtr := pointer.For([]int{})
		assert.NotNil(t, slicePtr)
		assert.Equal(t, []int{}, *slicePtr)

		// Map
		mapPtr := pointer.For(map[string]int{})
		assert.NotNil(t, mapPtr)
		assert.Equal(t, map[string]int{}, *mapPtr)
	})

	t.Run("nil values", func(t *testing.T) {
		// Nil slice
		var nilSlice []int
		slicePtr := pointer.For(nilSlice)
		assert.NotNil(t, slicePtr)
		assert.Nil(t, *slicePtr)

		// Nil map
		var nilMap map[string]int
		mapPtr := pointer.For(nilMap)
		assert.NotNil(t, mapPtr)
		assert.Nil(t, *mapPtr)

		// Nil interface
		var nilInterface interface{}
		interfacePtr := pointer.For(nilInterface)
		assert.NotNil(t, interfacePtr)
		assert.Nil(t, *interfacePtr)
	})
}
