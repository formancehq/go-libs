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

	// deprecated
	PageSizeDeprecated int `json:"page_size,omitempty"`
	// deprecated
	HasMoreDeprecated *bool `json:"has_more"`
}

type Total struct {
	Value uint64 `json:"value"`
	Rel   string `json:"relation"`
}

type ErrorResponse struct {
	ErrorCode    string `json:"errorCode,omitempty"`
	ErrorMessage string `json:"errorMessage,omitempty"`

	// deprecated
	ErrorCodeDeprecated string `json:"error_code,omitempty"`
	// deprecated
	ErrorMessageDeprecated string `json:"error_message,omitempty"`
}
