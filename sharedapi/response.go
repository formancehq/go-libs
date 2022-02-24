package sharedapi

type BaseResponse struct {
	Data   interface{} `json:"data,omitempty"`
	Cursor *Cursor     `json:"cursor,omitempty"`
}

type Cursor struct {
	PageSize  int         `json:"page_size,omitempty"`
	HasMore   bool        `json:"has_more"`
	Total     int64       `json:"total,omitempty"`
	Remaining int         `json:"remaining_results,omitempty"`
	Previous  string      `json:"previous,omitempty"`
	Next      string      `json:"next,omitempty"`
	Data      interface{} `json:"data"`
}

type ErrorResponse struct {
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
}
