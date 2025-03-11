package bunpaginate_test

import (
	"encoding/json"
	"testing"

	"github.com/formancehq/go-libs/v2/bun/bunpaginate"
	"github.com/stretchr/testify/require"
)

func TestCursor(t *testing.T) {
	t.Parallel()
	
	// Test basic cursor creation and JSON marshaling
	t.Run("cursor marshaling", func(t *testing.T) {
		t.Parallel()
		
		// Create a cursor with string data
		cursor := bunpaginate.Cursor[string]{
			PageSize: 10,
			HasMore:  true,
			Previous: "prev-token",
			Next:     "next-token",
			Data:     []string{"item1", "item2", "item3"},
		}
		
		// Marshal to JSON
		data, err := json.Marshal(cursor)
		require.NoError(t, err)
		
		// Unmarshal back to verify
		var unmarshaledCursor bunpaginate.Cursor[string]
		err = json.Unmarshal(data, &unmarshaledCursor)
		require.NoError(t, err)
		
		// Verify all fields match
		require.Equal(t, cursor.PageSize, unmarshaledCursor.PageSize)
		require.Equal(t, cursor.HasMore, unmarshaledCursor.HasMore)
		require.Equal(t, cursor.Previous, unmarshaledCursor.Previous)
		require.Equal(t, cursor.Next, unmarshaledCursor.Next)
		require.Equal(t, cursor.Data, unmarshaledCursor.Data)
	})
	
	// Test cursor with different data types
	t.Run("cursor with different types", func(t *testing.T) {
		t.Parallel()
		
		// Test with int type
		intCursor := bunpaginate.Cursor[int]{
			PageSize: 5,
			HasMore:  false,
			Data:     []int{1, 2, 3, 4, 5},
		}
		
		data, err := json.Marshal(intCursor)
		require.NoError(t, err)
		
		var unmarshaledIntCursor bunpaginate.Cursor[int]
		err = json.Unmarshal(data, &unmarshaledIntCursor)
		require.NoError(t, err)
		
		require.Equal(t, intCursor.Data, unmarshaledIntCursor.Data)
		
		// Test with struct type
		type TestItem struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		}
		
		structCursor := bunpaginate.Cursor[TestItem]{
			PageSize: 2,
			HasMore:  true,
			Data: []TestItem{
				{ID: 1, Name: "Item 1"},
				{ID: 2, Name: "Item 2"},
			},
		}
		
		data, err = json.Marshal(structCursor)
		require.NoError(t, err)
		
		var unmarshaledStructCursor bunpaginate.Cursor[TestItem]
		err = json.Unmarshal(data, &unmarshaledStructCursor)
		require.NoError(t, err)
		
		require.Equal(t, structCursor.Data, unmarshaledStructCursor.Data)
	})
	
	// Test empty cursor
	t.Run("empty cursor", func(t *testing.T) {
		t.Parallel()
		
		emptyCursor := bunpaginate.Cursor[string]{
			PageSize: 0,
			HasMore:  false,
			Data:     []string{},
		}
		
		data, err := json.Marshal(emptyCursor)
		require.NoError(t, err)
		
		var unmarshaledEmptyCursor bunpaginate.Cursor[string]
		err = json.Unmarshal(data, &unmarshaledEmptyCursor)
		require.NoError(t, err)
		
		require.Empty(t, unmarshaledEmptyCursor.Data)
		require.False(t, unmarshaledEmptyCursor.HasMore)
	})
}

func TestMapCursor(t *testing.T) {
	t.Parallel()
	
	testCases := []struct {
		name     string
		input    bunpaginate.Cursor[int]
		mapper   func(int) string
		expected bunpaginate.Cursor[string]
	}{
		{
			name: "basic mapping",
			input: bunpaginate.Cursor[int]{
				PageSize: 10,
				HasMore:  true,
				Previous: "prev-token",
				Next:     "next-token",
				Data:     []int{1, 2, 3},
			},
			mapper: func(i int) string {
				return "item-" + string(rune('0'+i))
			},
			expected: bunpaginate.Cursor[string]{
				PageSize: 10,
				HasMore:  true,
				Previous: "prev-token",
				Next:     "next-token",
				Data:     []string{"item-1", "item-2", "item-3"},
			},
		},
		{
			name: "empty data",
			input: bunpaginate.Cursor[int]{
				PageSize: 5,
				HasMore:  false,
				Data:     []int{},
			},
			mapper: func(i int) string {
				return "item-" + string(rune('0'+i))
			},
			expected: bunpaginate.Cursor[string]{
				PageSize: 5,
				HasMore:  false,
				Data:     []string{},
			},
		},
		{
			name: "complex mapping",
			input: bunpaginate.Cursor[int]{
				PageSize: 3,
				HasMore:  true,
				Data:     []int{10, 20, 30},
			},
			mapper: func(i int) string {
				return "value: " + string(rune('0'+(i/10)))
			},
			expected: bunpaginate.Cursor[string]{
				PageSize: 3,
				HasMore:  true,
				Data:     []string{"value: 1", "value: 2", "value: 3"},
			},
		},
	}
	
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			
			result := bunpaginate.MapCursor(&tc.input, tc.mapper)
			
			// Verify all fields match
			require.Equal(t, tc.expected.PageSize, result.PageSize)
			require.Equal(t, tc.expected.HasMore, result.HasMore)
			require.Equal(t, tc.expected.Previous, result.Previous)
			require.Equal(t, tc.expected.Next, result.Next)
			require.Equal(t, tc.expected.Data, result.Data)
		})
	}
	
	// Test with struct types
	t.Run("struct mapping", func(t *testing.T) {
		t.Parallel()
		
		type InputType struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		}
		
		type OutputType struct {
			ID      int    `json:"id"`
			Name    string `json:"name"`
			Display string `json:"display"`
		}
		
		inputCursor := bunpaginate.Cursor[InputType]{
			PageSize: 2,
			HasMore:  true,
			Data: []InputType{
				{ID: 1, Name: "Item 1"},
				{ID: 2, Name: "Item 2"},
			},
		}
		
		mapper := func(input InputType) OutputType {
			return OutputType{
				ID:      input.ID,
				Name:    input.Name,
				Display: input.Name + " (ID: " + string(rune('0'+input.ID)) + ")",
			}
		}
		
		result := bunpaginate.MapCursor(&inputCursor, mapper)
		
		require.Equal(t, 2, len(result.Data))
		require.Equal(t, "Item 1", result.Data[0].Name)
		require.Equal(t, "Item 1 (ID: 1)", result.Data[0].Display)
		require.Equal(t, "Item 2", result.Data[1].Name)
		require.Equal(t, "Item 2 (ID: 2)", result.Data[1].Display)
	})
}
