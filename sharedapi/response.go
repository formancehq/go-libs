package sharedapi

import "encoding/json"

type BaseResponse struct {
	Data   interface{} `json:"data,omitempty"`
	Cursor *Cursor     `json:"cursor,omitempty"`
}

type Cursor struct {
	PageSize int         `json:"page_size,omitempty"`
	HasMore  bool        `json:"has_more"`
	Previous string      `json:"previous,omitempty"`
	Next     string      `json:"next,omitempty"`
	Data     interface{} `json:"data"`
}

func (c Cursor) MarshalJSON() ([]byte, error) {
	type Aux Cursor
	return json.Marshal(struct {
		Aux
		// Keep those fields to ensure backward compatibility, even if it will be
		Total     int64 `json:"total,omitempty"`
		Remaining int   `json:"remaining_results,omitempty"`
	}{
		Aux: Aux(c),
	})
}

type ErrorResponse struct {
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
}
