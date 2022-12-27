package sharedapi

type BaseResponse[T any] struct {
	Data   *T         `json:"data,omitempty"`
	Cursor *Cursor[T] `json:"cursor,omitempty"`
}

type Cursor[T any] struct {
	PageSize int    `json:"pageSize,omitempty"`
	Total    Total  `json:"total,omitempty"`
	HasMore  bool   `json:"hasMore"`
	Previous string `json:"previous,omitempty"`
	Next     string `json:"next,omitempty"`
	Data     []T    `json:"data"`
}

type Total struct {
	Value uint64 `json:"value"`
	Rel   string `json:"relation"`
}

type ErrorResponse struct {
	ErrorCode    string `json:"errorCode,omitempty"`
	ErrorMessage string `json:"errorMessage,omitempty"`
}

// BaseResponseDeprecated is deprecated: use sharedapi.BaseResponse instead
//
// TODO: to be removed after a v2 release of the ledger
type BaseResponseDeprecated[T any] struct {
	Data   *T                   `json:"data,omitempty"`
	Cursor *CursorDeprecated[T] `json:"cursor,omitempty"`
}

// CursorDeprecated is deprecated: use sharedapi.Cursor instead
//
// TODO: to be removed after a v2 release of the ledger
type CursorDeprecated[T any] struct {
	PageSize int    `json:"page_size,omitempty"`
	HasMore  bool   `json:"has_more"`
	Previous string `json:"previous,omitempty"`
	Next     string `json:"next,omitempty"`
	Data     []T    `json:"data"`
}

// ErrorResponseDeprecated is deprecated: use sharedapi.ErrorResponse instead
//
// TODO: to be removed after a v2 release of the ledger
type ErrorResponseDeprecated struct {
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
}
