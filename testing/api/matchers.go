package api

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/onsi/gomega/types"
	"github.com/pkg/errors"

	"github.com/formancehq/go-libs/v4/api"
)

type HaveErrorCodeMatcher struct {
	lastSeen        string
	lastSeenMessage string
	expected        string
}

func (s *HaveErrorCodeMatcher) Match(actual interface{}) (success bool, err error) {

	errorResponse := api.ErrorResponse{}
	switch {
	case errors.Is(err, errorResponse):
		if !errors.As(err, &errorResponse) {
			return false, nil
		}
	default:
		data, err := json.Marshal(actual)
		if err != nil {
			return false, err
		}
		if err = json.Unmarshal(data, &errorResponse); err != nil {
			return false, err
		}
		if errorResponse.ErrorCode == "" {
			return false, nil
		}
	}

	s.lastSeen = errorResponse.ErrorCode
	s.lastSeenMessage = errorResponse.ErrorMessage

	return reflect.DeepEqual(errorResponse.ErrorCode, s.expected), nil
}

func (s *HaveErrorCodeMatcher) FailureMessage(actual interface{}) (message string) {
	if actual == nil {
		return fmt.Sprintf("error should have code %s but is nil", s.expected)
	}
	return fmt.Sprintf("error should have code %s but have %s with message '%s'", s.expected, s.lastSeen, s.lastSeenMessage)
}

func (s *HaveErrorCodeMatcher) NegatedFailureMessage(actual interface{}) (message string) {
	return fmt.Sprintf("error should not have code %s", s.expected)
}

var _ types.GomegaMatcher = (*HaveErrorCodeMatcher)(nil)

func HaveErrorCode(expected string) *HaveErrorCodeMatcher {
	return &HaveErrorCodeMatcher{
		expected: expected,
	}
}
