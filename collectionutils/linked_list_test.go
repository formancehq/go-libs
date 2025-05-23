package collectionutils_test

import (
	"sync"
	"testing"

	"github.com/formancehq/go-libs/v3/collectionutils"
	"github.com/stretchr/testify/require"
)

func TestLinkedList(t *testing.T) {
	t.Parallel()
	t.Run("NewLinkedList", func(t *testing.T) {
		t.Parallel()
		list := collectionutils.NewLinkedList[int]()
		require.NotNil(t, list)
		require.Equal(t, 0, list.Length())
		require.Nil(t, list.FirstNode())
	})

	t.Run("Append", func(t *testing.T) {
		t.Parallel()
		list := collectionutils.NewLinkedList[int]()

		// Append to empty list
		list.Append(1)
		require.Equal(t, 1, list.Length())
		require.NotNil(t, list.FirstNode())
		require.Equal(t, 1, list.FirstNode().Value())

		// Append to non-empty list
		list.Append(2)
		require.Equal(t, 2, list.Length())
		require.Equal(t, 1, list.FirstNode().Value())
		require.Equal(t, 2, list.FirstNode().Next().Value())

		// Append multiple values
		list.Append(3, 4, 5)
		require.Equal(t, 5, list.Length())

		// Verify the order of elements
		expected := []int{1, 2, 3, 4, 5}
		node := list.FirstNode()
		for i := 0; i < 5; i++ {
			require.Equal(t, expected[i], node.Value())
			node = node.Next()
		}
		require.Nil(t, node) // After the last node
	})

	t.Run("RemoveFirst", func(t *testing.T) {
		t.Parallel()
		list := collectionutils.NewLinkedList[int]()
		list.Append(1, 2, 3, 4, 5)

		// Remove first element matching condition
		node := list.RemoveFirst(func(i int) bool {
			return i == 3
		})

		require.NotNil(t, node)
		require.Equal(t, 3, node.Value())
		require.Equal(t, 4, list.Length())

		// Verify the order of remaining elements
		expected := []int{1, 2, 4, 5}
		actualSlice := list.Slice()
		require.Equal(t, expected, actualSlice)

		// Remove first element
		node = list.RemoveFirst(func(i int) bool {
			return i == 1
		})

		require.NotNil(t, node)
		require.Equal(t, 1, node.Value())
		require.Equal(t, 3, list.Length())
		require.Equal(t, 2, list.FirstNode().Value())

		// Remove last element
		node = list.RemoveFirst(func(i int) bool {
			return i == 5
		})

		require.NotNil(t, node)
		require.Equal(t, 5, node.Value())
		require.Equal(t, 2, list.Length())

		// Try to remove non-existent element
		node = list.RemoveFirst(func(i int) bool {
			return i == 99
		})

		require.Nil(t, node)
		require.Equal(t, 2, list.Length())
	})

	t.Run("RemoveValue", func(t *testing.T) {
		t.Parallel()
		list := collectionutils.NewLinkedList[int]()
		list.Append(1, 2, 3, 4, 5)

		// Remove existing value
		node := list.RemoveValue(3)
		require.NotNil(t, node)
		require.Equal(t, 3, node.Value())
		require.Equal(t, 4, list.Length())

		// Verify the order of remaining elements
		expected := []int{1, 2, 4, 5}
		actualSlice := list.Slice()
		require.Equal(t, expected, actualSlice)

		// Remove non-existent value
		node = list.RemoveValue(99)
		require.Nil(t, node)
		require.Equal(t, 4, list.Length())
	})

	t.Run("TakeFirst", func(t *testing.T) {
		t.Parallel()
		list := collectionutils.NewLinkedList[int]()
		list.Append(1, 2, 3)

		// Take first element
		value := list.TakeFirst()
		require.Equal(t, 1, value)
		require.Equal(t, 2, list.Length())
		require.Equal(t, 2, list.FirstNode().Value())

		// Take another element
		value = list.TakeFirst()
		require.Equal(t, 2, value)
		require.Equal(t, 1, list.Length())
		require.Equal(t, 3, list.FirstNode().Value())

		// Take last element
		value = list.TakeFirst()
		require.Equal(t, 3, value)
		require.Equal(t, 0, list.Length())
		require.Nil(t, list.FirstNode())

		// Take from empty list
		value = list.TakeFirst()
		require.Equal(t, 0, value) // Zero value for int
		require.Equal(t, 0, list.Length())
		require.Nil(t, list.FirstNode())
	})

	t.Run("Length", func(t *testing.T) {
		t.Parallel()
		list := collectionutils.NewLinkedList[int]()
		require.Equal(t, 0, list.Length())

		list.Append(1)
		require.Equal(t, 1, list.Length())

		list.Append(2, 3, 4)
		require.Equal(t, 4, list.Length())

		list.RemoveFirst(func(i int) bool {
			return i == 2
		})
		require.Equal(t, 3, list.Length())

		list.TakeFirst()
		require.Equal(t, 2, list.Length())
	})

	t.Run("ForEach", func(t *testing.T) {
		t.Parallel()
		list := collectionutils.NewLinkedList[int]()
		list.Append(1, 2, 3, 4, 5)

		sum := 0
		list.ForEach(func(i int) {
			sum += i
		})

		require.Equal(t, 15, sum)

		// ForEach on empty list
		emptyList := collectionutils.NewLinkedList[int]()
		called := false
		emptyList.ForEach(func(i int) {
			called = true
		})

		require.False(t, called)
	})

	t.Run("Slice", func(t *testing.T) {
		t.Parallel()
		list := collectionutils.NewLinkedList[int]()
		list.Append(1, 2, 3, 4, 5)

		slice := list.Slice()
		require.Equal(t, []int{1, 2, 3, 4, 5}, slice)

		// Slice of empty list
		emptyList := collectionutils.NewLinkedList[int]()
		emptySlice := emptyList.Slice()
		require.Equal(t, []int{}, emptySlice)
	})

	t.Run("LinkedListNode", func(t *testing.T) {
		t.Parallel()
		list := collectionutils.NewLinkedList[int]()
		list.Append(1, 2, 3)

		// Test Next() and Value()
		node := list.FirstNode()
		require.Equal(t, 1, node.Value())

		node = node.Next()
		require.Equal(t, 2, node.Value())

		node = node.Next()
		require.Equal(t, 3, node.Value())

		node = node.Next()
		require.Nil(t, node)

		// Test Remove() on middle node
		list = collectionutils.NewLinkedList[int]()
		list.Append(1, 2, 3)

		node = list.FirstNode().Next() // Node with value 2
		node.Remove()

		require.Equal(t, 2, list.Length())
		require.Equal(t, []int{1, 3}, list.Slice())

		// Test Remove() on first node
		list = collectionutils.NewLinkedList[int]()
		list.Append(1, 2, 3)

		node = list.FirstNode() // Node with value 1
		node.Remove()

		require.Equal(t, 2, list.Length())
		require.Equal(t, []int{2, 3}, list.Slice())

		// Test Remove() on last node
		list = collectionutils.NewLinkedList[int]()
		list.Append(1, 2, 3)

		node = list.FirstNode().Next().Next() // Node with value 3
		node.Remove()

		require.Equal(t, 2, list.Length())
		require.Equal(t, []int{1, 2}, list.Slice())

		// Test Remove() on single node
		list = collectionutils.NewLinkedList[int]()
		list.Append(1)

		node = list.FirstNode() // Node with value 1
		node.Remove()

		require.Equal(t, 0, list.Length())
		require.Equal(t, []int{}, list.Slice())
	})

	t.Run("Concurrency", func(t *testing.T) {
		t.Parallel()
		list := collectionutils.NewLinkedList[int]()

		// Test concurrent appends
		var wg sync.WaitGroup
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func(val int) {
				defer wg.Done()
				list.Append(val)
			}(i)
		}
		wg.Wait()

		require.Equal(t, 100, list.Length())

		// Test concurrent reads
		var sum int
		var mu sync.Mutex
		wg = sync.WaitGroup{}

		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				localSum := 0
				list.ForEach(func(i int) {
					localSum += i
				})
				mu.Lock()
				sum += localSum
				mu.Unlock()
			}()
		}
		wg.Wait()

		// Sum should be 10 * sum of numbers from 0 to 99
		expectedSum := 10 * (99 * 100 / 2)
		require.Equal(t, expectedSum, sum)
	})
}
