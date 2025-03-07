package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/formancehq/go-libs/v2/api"
	"github.com/formancehq/go-libs/v2/bun/bunpaginate"
	"github.com/formancehq/go-libs/v2/logging"
	"github.com/stretchr/testify/require"
)

func TestWriteErrorResponse(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name       string
		statusCode int
		errorCode  string
		err        error
		expected   api.ErrorResponse
	}{
		{
			name:       "basic error",
			statusCode: http.StatusBadRequest,
			errorCode:  "INVALID_REQUEST",
			err:        errors.New("invalid request"),
			expected: api.ErrorResponse{
				ErrorCode:    "INVALID_REQUEST",
				ErrorMessage: "invalid request",
			},
		},
		{
			name:       "not found error",
			statusCode: http.StatusNotFound,
			errorCode:  api.ErrorCodeNotFound,
			err:        errors.New("resource not found"),
			expected: api.ErrorResponse{
				ErrorCode:    api.ErrorCodeNotFound,
				ErrorMessage: "resource not found",
			},
		},
		{
			name:       "internal error",
			statusCode: http.StatusInternalServerError,
			errorCode:  api.ErrorInternal,
			err:        errors.New("internal server error"),
			expected: api.ErrorResponse{
				ErrorCode:    api.ErrorInternal,
				ErrorMessage: "internal server error",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Create a response recorder
			rr := httptest.NewRecorder()

			// Call the function
			api.WriteErrorResponse(rr, tc.statusCode, tc.errorCode, tc.err)

			// Check the status code
			require.Equal(t, tc.statusCode, rr.Code)

			// Check the content type
			require.Equal(t, "application/json", rr.Header().Get("Content-Type"))

			// Check the response body
			var response api.ErrorResponse
			err := json.NewDecoder(rr.Body).Decode(&response)
			require.NoError(t, err)
			require.Equal(t, tc.expected, response)
		})
	}
}

func TestNotFound(t *testing.T) {
	t.Parallel()
	// Create a response recorder
	rr := httptest.NewRecorder()
	err := errors.New("resource not found")

	// Call the function
	api.NotFound(rr, err)

	// Check the status code
	require.Equal(t, http.StatusNotFound, rr.Code)

	// Check the content type
	require.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	// Check the response body
	var response api.ErrorResponse
	decodeErr := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, decodeErr)
	require.Equal(t, api.ErrorCodeNotFound, response.ErrorCode)
	require.Equal(t, err.Error(), response.ErrorMessage)
}

func TestNoContent(t *testing.T) {
	t.Parallel()
	// Create a response recorder
	rr := httptest.NewRecorder()

	// Call the function
	api.NoContent(rr)

	// Check the status code
	require.Equal(t, http.StatusNoContent, rr.Code)

	// Check that the body is empty
	require.Empty(t, rr.Body.String())
}

func TestForbidden(t *testing.T) {
	t.Parallel()
	// Create a response recorder
	rr := httptest.NewRecorder()
	err := errors.New("access denied")
	code := "ACCESS_DENIED"

	// Call the function
	api.Forbidden(rr, code, err)

	// Check the status code
	require.Equal(t, http.StatusForbidden, rr.Code)

	// Check the content type
	require.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	// Check the response body
	var response api.ErrorResponse
	decodeErr := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, decodeErr)
	require.Equal(t, code, response.ErrorCode)
	require.Equal(t, err.Error(), response.ErrorMessage)
}

func TestBadRequest(t *testing.T) {
	t.Parallel()
	// Create a response recorder
	rr := httptest.NewRecorder()
	err := errors.New("invalid request")
	code := "INVALID_REQUEST"

	// Call the function
	api.BadRequest(rr, code, err)

	// Check the status code
	require.Equal(t, http.StatusBadRequest, rr.Code)

	// Check the content type
	require.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	// Check the response body
	var response api.ErrorResponse
	decodeErr := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, decodeErr)
	require.Equal(t, code, response.ErrorCode)
	require.Equal(t, err.Error(), response.ErrorMessage)
}

func TestBadRequestWithDetails(t *testing.T) {
	t.Parallel()
	// Create a response recorder
	rr := httptest.NewRecorder()
	err := errors.New("invalid request")
	code := "INVALID_REQUEST"
	details := "Additional error details"

	// Call the function
	api.BadRequestWithDetails(rr, code, err, details)

	// Check the status code
	require.Equal(t, http.StatusBadRequest, rr.Code)

	// Check the content type
	require.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	// Check the response body
	var response api.ErrorResponse
	decodeErr := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, decodeErr)
	require.Equal(t, code, response.ErrorCode)
	require.Equal(t, err.Error(), response.ErrorMessage)
	require.Equal(t, details, response.Details)
}

func TestInternalServerError(t *testing.T) {
	t.Parallel()
	// Create a buffer to capture log output
	var buf bytes.Buffer
	logger := logging.NewDefaultLogger(&buf, true, false, false)

	// Create a request with the logger in context
	req, err := http.NewRequest("GET", "/test", nil)
	require.NoError(t, err)
	ctx := logging.ContextWithLogger(context.Background(), logger)
	req = req.WithContext(ctx)

	// Create a response recorder
	rr := httptest.NewRecorder()
	testErr := errors.New("internal server error")

	// Call the function
	api.InternalServerError(rr, req, testErr)

	// Check the status code
	require.Equal(t, http.StatusInternalServerError, rr.Code)

	// Check the content type
	require.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	// Check the response body
	var response api.ErrorResponse
	decodeErr := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, decodeErr)
	require.Equal(t, api.ErrorInternal, response.ErrorCode)
	require.Equal(t, testErr.Error(), response.ErrorMessage)

	// Verify that the error was logged
	logOutput := buf.String()
	require.Contains(t, logOutput, testErr.Error())
}

func TestAccepted(t *testing.T) {
	t.Parallel()
	// Create a response recorder
	rr := httptest.NewRecorder()
	data := map[string]string{"message": "accepted"}

	// Call the function
	api.Accepted(rr, data)

	// Check the status code
	require.Equal(t, http.StatusAccepted, rr.Code)

	// Check the content type
	require.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	// Check the response body
	var response api.BaseResponse[any]
	decodeErr := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, decodeErr)
	require.NotNil(t, response.Data)

	// Convert the data to a map
	dataMap, ok := (*response.Data).(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "accepted", dataMap["message"])
}

func TestCreated(t *testing.T) {
	t.Parallel()
	// Create a response recorder
	rr := httptest.NewRecorder()
	data := map[string]string{"id": "123", "name": "test"}

	// Call the function
	api.Created(rr, data)

	// Check the status code
	require.Equal(t, http.StatusCreated, rr.Code)

	// Check the content type
	require.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	// Check the response body
	var response api.BaseResponse[any]
	decodeErr := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, decodeErr)
	require.NotNil(t, response.Data)

	// Convert the data to a map
	dataMap, ok := (*response.Data).(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "123", dataMap["id"])
	require.Equal(t, "test", dataMap["name"])
}

func TestRawOk(t *testing.T) {
	t.Parallel()
	// Create a response recorder
	rr := httptest.NewRecorder()
	data := map[string]string{"message": "success"}

	// Call the function
	api.RawOk(rr, data)

	// Check the status code
	require.Equal(t, http.StatusOK, rr.Code)

	// Check the content type
	require.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	// Check the response body
	var response map[string]string
	decodeErr := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, decodeErr)
	require.Equal(t, "success", response["message"])
}

func TestOk(t *testing.T) {
	t.Parallel()
	// Create a response recorder
	rr := httptest.NewRecorder()
	data := map[string]string{"message": "success"}

	// Call the function
	api.Ok(rr, data)

	// Check the status code
	require.Equal(t, http.StatusOK, rr.Code)

	// Check the content type
	require.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	// Check the response body
	var response api.BaseResponse[any]
	decodeErr := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, decodeErr)
	require.NotNil(t, response.Data)

	// Convert the data to a map
	dataMap, ok := (*response.Data).(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "success", dataMap["message"])
}

func TestRenderCursor(t *testing.T) {
	t.Parallel()
	// Create a response recorder
	rr := httptest.NewRecorder()
	cursor := bunpaginate.Cursor[string]{
		PageSize: 10,
		HasMore:  true,
		Next:     "next-token",
		Previous: "prev-token",
		Data:     []string{"item1", "item2", "item3"},
	}

	// Call the function
	api.RenderCursor(rr, cursor)

	// Check the status code
	require.Equal(t, http.StatusOK, rr.Code)

	// Check the content type
	require.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	// Check the response body
	var response api.BaseResponse[string]
	decodeErr := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, decodeErr)
	require.Nil(t, response.Data)
	require.NotNil(t, response.Cursor)
	require.Equal(t, cursor.PageSize, response.Cursor.PageSize)
	require.Equal(t, cursor.HasMore, response.Cursor.HasMore)
	require.Equal(t, cursor.Next, response.Cursor.Next)
	require.Equal(t, cursor.Previous, response.Cursor.Previous)
	require.Equal(t, cursor.Data, response.Cursor.Data)
}

func TestWriteResponse(t *testing.T) {
	t.Parallel()
	// Create a response recorder
	rr := httptest.NewRecorder()
	body := []byte(`{"message":"test"}`)

	// Call the function
	api.WriteResponse(rr, http.StatusOK, body)

	// Check the status code
	require.Equal(t, http.StatusOK, rr.Code)

	// Check the response body
	require.Equal(t, string(body), rr.Body.String())
}

func TestCursorFromListResponse(t *testing.T) {
	t.Parallel()
	// Create a response recorder
	rr := httptest.NewRecorder()

	// Create a list query
	query := api.ListQuery[string]{
		Pagination: api.Pagination{
			Limit:           10,
			PaginationToken: "token",
		},
		Payload: "test",
	}

	// Create a list response
	response := &api.ListResponse[string]{
		Data:     []string{"item1", "item2", "item3"},
		Next:     "next-token",
		Previous: "prev-token",
		HasMore:  true,
	}

	// Call the function
	api.CursorFromListResponse(rr, query, response)

	// Check the status code
	require.Equal(t, http.StatusOK, rr.Code)

	// Check the content type
	require.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	// Check the response body
	var apiResponse api.BaseResponse[string]
	decodeErr := json.NewDecoder(rr.Body).Decode(&apiResponse)
	require.NoError(t, decodeErr)
	require.Nil(t, apiResponse.Data)
	require.NotNil(t, apiResponse.Cursor)
	require.Equal(t, query.Limit, apiResponse.Cursor.PageSize)
	require.Equal(t, response.HasMore, apiResponse.Cursor.HasMore)
	require.Equal(t, response.Next, apiResponse.Cursor.Next)
	require.Equal(t, response.Previous, apiResponse.Cursor.Previous)
	require.Equal(t, response.Data, apiResponse.Cursor.Data)
}

func TestParsePaginationToken(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name           string
		queryParams    map[string]string
		expectedResult string
	}{
		{
			name: "with pagination token",
			queryParams: map[string]string{
				"paginationToken": "test-token",
			},
			expectedResult: "test-token",
		},
		{
			name:           "without pagination token",
			queryParams:    map[string]string{},
			expectedResult: "",
		},
		{
			name: "with empty pagination token",
			queryParams: map[string]string{
				"paginationToken": "",
			},
			expectedResult: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Create a URL with query parameters
			u, _ := url.Parse("http://example.com")
			q := u.Query()
			for k, v := range tc.queryParams {
				q.Set(k, v)
			}
			u.RawQuery = q.Encode()

			// Create a request with the URL
			req, _ := http.NewRequest("GET", u.String(), nil)

			// Call the function
			result := api.ParsePaginationToken(req)

			// Check the result
			require.Equal(t, tc.expectedResult, result)
		})
	}
}

func TestParsePageSize(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name           string
		queryParams    map[string]string
		expectedResult int
	}{
		{
			name: "with page size",
			queryParams: map[string]string{
				"pageSize": "20",
			},
			expectedResult: 20,
		},
		{
			name:           "without page size",
			queryParams:    map[string]string{},
			expectedResult: 15, // default limit
		},
		{
			name: "with empty page size",
			queryParams: map[string]string{
				"pageSize": "",
			},
			expectedResult: 15, // default limit
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Create a URL with query parameters
			u, _ := url.Parse("http://example.com")
			q := u.Query()
			for k, v := range tc.queryParams {
				q.Set(k, v)
			}
			u.RawQuery = q.Encode()

			// Create a request with the URL
			req, _ := http.NewRequest("GET", u.String(), nil)

			// Call the function
			result := api.ParsePageSize(req)

			// Check the result
			require.Equal(t, tc.expectedResult, result)
		})
	}

	// Test panic with invalid page size
	t.Run("invalid page size", func(t *testing.T) {
		// Create a URL with invalid page size
		u, _ := url.Parse("http://example.com")
		q := u.Query()
		q.Set("pageSize", "invalid")
		u.RawQuery = q.Encode()

		// Create a request with the URL
		req, _ := http.NewRequest("GET", u.String(), nil)

		// Call the function and expect panic
		require.Panics(t, func() {
			api.ParsePageSize(req)
		})
	})
}

func TestReadPaginatedRequest(t *testing.T) {
	t.Parallel()
	// Create a URL with query parameters
	u, _ := url.Parse("http://example.com")
	q := u.Query()
	q.Set("pageSize", "20")
	q.Set("paginationToken", "test-token")
	u.RawQuery = q.Encode()

	// Create a request with the URL
	req, _ := http.NewRequest("GET", u.String(), nil)

	// Define a payload extractor function
	type TestPayload struct {
		Filter string
	}
	payloadExtractor := func(r *http.Request) TestPayload {
		return TestPayload{
			Filter: r.URL.Query().Get("filter"),
		}
	}

	// Call the function
	result := api.ReadPaginatedRequest(req, payloadExtractor)

	// Check the result
	require.Equal(t, 20, result.Limit)
	require.Equal(t, "test-token", result.PaginationToken)
	require.Equal(t, TestPayload{Filter: ""}, result.Payload)

	// Test with nil payload extractor
	result = api.ReadPaginatedRequest[TestPayload](req, nil)
	require.Equal(t, 20, result.Limit)
	require.Equal(t, "test-token", result.PaginationToken)
	require.Equal(t, TestPayload{}, result.Payload)
}

func TestGetQueryMap(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name           string
		queryParams    map[string][]string
		key            string
		expectedResult map[string]string
	}{
		{
			name: "with query map",
			queryParams: map[string][]string{
				"filter[name]":  {"john"},
				"filter[age]":   {"30"},
				"filter[email]": {"john@example.com"},
				"sort":          {"name"},
			},
			key: "filter",
			expectedResult: map[string]string{
				"name":  "john",
				"age":   "30",
				"email": "john@example.com",
			},
		},
		{
			name: "without query map",
			queryParams: map[string][]string{
				"sort": {"name"},
			},
			key:            "filter",
			expectedResult: map[string]string{},
		},
		{
			name: "with empty query map",
			queryParams: map[string][]string{
				"filter[]": {"empty"},
			},
			key:            "filter",
			expectedResult: map[string]string{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Call the function
			result := api.GetQueryMap(tc.queryParams, tc.key)

			// Check the result
			require.Equal(t, tc.expectedResult, result)
		})
	}
}

func TestFetchAllPaginated(t *testing.T) {
	t.Parallel()
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cursor := r.URL.Query().Get("cursor")

		// First request
		if cursor == "" {
			response := api.BaseResponse[string]{
				Cursor: &bunpaginate.Cursor[string]{
					HasMore: true,
					Next:    "next-token",
					Data:    []string{"item1", "item2"},
				},
			}
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(response)
			return
		}

		// Second request
		if cursor == "next-token" {
			response := api.BaseResponse[string]{
				Cursor: &bunpaginate.Cursor[string]{
					HasMore: false,
					Data:    []string{"item3", "item4"},
				},
			}
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(response)
			return
		}

		// Unexpected cursor
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	// Create a client
	client := server.Client()

	// Call the function
	ctx := context.Background()
	queryParams := url.Values{
		"param1": []string{"value1"},
	}

	result, err := api.FetchAllPaginated[string](ctx, client, server.URL, queryParams)

	// Check the result
	require.NoError(t, err)
	require.Equal(t, []string{"item1", "item2", "item3", "item4"}, result)

	// Test with error response
	errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer errorServer.Close()

	_, err = api.FetchAllPaginated[string](ctx, errorServer.Client(), errorServer.URL, queryParams)
	require.Error(t, err)

	// Test with invalid JSON response
	invalidServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("invalid json"))
	}))
	defer invalidServer.Close()

	_, err = api.FetchAllPaginated[string](ctx, invalidServer.Client(), invalidServer.URL, queryParams)
	require.Error(t, err)
}

func TestErrorResponse(t *testing.T) {
	t.Parallel()
	// Test Error method
	errorResponse := api.ErrorResponse{
		ErrorCode:    "TEST_ERROR",
		ErrorMessage: "test error message",
	}

	require.Equal(t, "[TEST_ERROR] test error message", errorResponse.Error())
}

func TestResponseUtils(t *testing.T) {
	t.Run("Encode", func(t *testing.T) {
		t.Parallel()
		data := map[string]string{"key": "value"}
		encoded := api.Encode(t, data)
		require.Equal(t, `{"key":"value"}`, string(encoded))
	})

	t.Run("Buffer", func(t *testing.T) {
		t.Parallel()
		data := map[string]string{"key": "value"}
		buffer := api.Buffer(t, data)
		require.Equal(t, `{"key":"value"}`, buffer.String())
	})

	t.Run("Decode", func(t *testing.T) {
		t.Parallel()
		data := map[string]string{"key": "value"}
		buffer := bytes.NewBufferString(`{"key":"value"}`)
		var result map[string]string
		api.Decode(t, buffer, &result)
		require.Equal(t, data, result)
	})

	t.Run("DecodeSingleResponse", func(t *testing.T) {
		t.Parallel()
		buffer := bytes.NewBufferString(`{"data":{"key":"value"}}`)
		result, ok := api.DecodeSingleResponse[map[string]string](t, buffer)
		require.True(t, ok)
		require.Equal(t, map[string]string{"key": "value"}, result)
	})

	t.Run("DecodeCursorResponse", func(t *testing.T) {
		t.Parallel()
		buffer := bytes.NewBufferString(`{"cursor":{"hasMore":true,"data":["item1","item2"]}}`)
		cursor := api.DecodeCursorResponse[string](t, buffer)
		require.NotNil(t, cursor)
		require.True(t, cursor.HasMore)
		require.Equal(t, []string{"item1", "item2"}, cursor.Data)
	})
}
