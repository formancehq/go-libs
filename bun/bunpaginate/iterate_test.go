package bunpaginate_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/formancehq/go-libs/v2/bun/bunpaginate"
	"github.com/stretchr/testify/require"
)

// Query type for testing
type TestQuery struct {
	Page     int    `json:"page"`
	PageSize int    `json:"pageSize"`
	Token    string `json:"token,omitempty"`
}

// Test data type
type TestData struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func TestIterate(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		initialQuery   TestQuery
		totalItems     int
		pageSize       int
		expectedCalls  int
		expectedError  bool
		iteratorError  bool
		callbackError  bool
		expectedResult []TestData
	}{
		{
			name:          "single page",
			initialQuery:  TestQuery{Page: 1, PageSize: 10},
			totalItems:    5,
			pageSize:      10,
			expectedCalls: 1,
			expectedError: false,
			expectedResult: []TestData{
				{ID: 1, Name: "Item 1"},
				{ID: 2, Name: "Item 2"},
				{ID: 3, Name: "Item 3"},
				{ID: 4, Name: "Item 4"},
				{ID: 5, Name: "Item 5"},
			},
		},
		{
			name:          "multiple pages",
			initialQuery:  TestQuery{Page: 1, PageSize: 2},
			totalItems:    5,
			pageSize:      2,
			expectedCalls: 3,
			expectedError: false,
			expectedResult: []TestData{
				{ID: 1, Name: "Item 1"},
				{ID: 2, Name: "Item 2"},
				{ID: 3, Name: "Item 3"},
				{ID: 4, Name: "Item 4"},
				{ID: 5, Name: "Item 5"},
			},
		},
		{
			name:           "iterator error",
			initialQuery:   TestQuery{Page: 1, PageSize: 10},
			totalItems:     5,
			pageSize:       10,
			expectedCalls:  1,
			expectedError:  true,
			iteratorError:  true,
			expectedResult: nil,
		},
		{
			name:           "callback error",
			initialQuery:   TestQuery{Page: 1, PageSize: 10},
			totalItems:     5,
			pageSize:       10,
			expectedCalls:  1,
			expectedError:  true,
			callbackError:  true,
			expectedResult: nil,
		},
		{
			name:          "empty result",
			initialQuery:  TestQuery{Page: 1, PageSize: 10},
			totalItems:    0,
			pageSize:      10,
			expectedCalls: 1,
			expectedError: false,
			expectedResult: []TestData{},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Create test data
			allItems := make([]TestData, 0, tc.totalItems)
			for i := 1; i <= tc.totalItems; i++ {
				allItems = append(allItems, TestData{
					ID:   i,
					Name: fmt.Sprintf("Item %d", i),
				})
			}

			// Track calls to iterator
			calls := 0

			// Create iterator function
			iterator := func(ctx context.Context, q TestQuery) (*bunpaginate.Cursor[TestData], error) {
				calls++

				if tc.iteratorError {
					return nil, fmt.Errorf("iterator error")
				}

				// Calculate start and end indices for this page
				startIdx := (q.Page - 1) * tc.pageSize
				endIdx := startIdx + tc.pageSize
				if endIdx > tc.totalItems {
					endIdx = tc.totalItems
				}

				// Get items for this page
				var items []TestData
				if startIdx < tc.totalItems {
					items = allItems[startIdx:endIdx]
				} else {
					items = []TestData{}
				}

				// Determine if there are more pages
				hasMore := endIdx < tc.totalItems

				// Create cursor
				cursor := &bunpaginate.Cursor[TestData]{
					PageSize: tc.pageSize,
					HasMore:  hasMore,
					Data:     items,
				}

			// Set next token if there are more pages
			if hasMore {
				nextQuery := TestQuery{
					Page:     q.Page + 1,
					PageSize: tc.pageSize,
				}
				// Use EncodeCursor to properly encode the cursor
				cursor.Next = bunpaginate.EncodeCursor(nextQuery)
			}

				return cursor, nil
			}

			// Collect results
			result := []TestData{}

			// Create callback function
			callback := func(cursor *bunpaginate.Cursor[TestData]) error {
				if tc.callbackError {
					return fmt.Errorf("callback error")
				}
				result = append(result, cursor.Data...)
				return nil
			}

			// Call Iterate
			err := bunpaginate.Iterate(context.Background(), tc.initialQuery, iterator, callback)

			// Verify results
			if tc.expectedError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.expectedCalls, calls)
				require.Equal(t, tc.expectedResult, result)
			}
		})
	}
}

func TestIterateWithInvalidCursor(t *testing.T) {
	t.Parallel()

	// Create a test case where the cursor contains invalid base64 data
	iterator := func(ctx context.Context, q TestQuery) (*bunpaginate.Cursor[TestData], error) {
		cursor := &bunpaginate.Cursor[TestData]{
			PageSize: 10,
			HasMore:  true,
			Data:     []TestData{{ID: 1, Name: "Item 1"}},
			Next:     "invalid base64 data", // This will cause an error when trying to decode
		}
		return cursor, nil
	}

	callback := func(cursor *bunpaginate.Cursor[TestData]) error {
		return nil
	}

	// Call Iterate
	err := bunpaginate.Iterate(context.Background(), TestQuery{Page: 1, PageSize: 10}, iterator, callback)

	// Verify error
	require.Error(t, err)
	require.Contains(t, err.Error(), "paginating next request")
}
