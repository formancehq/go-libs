package health

import (
	"encoding/json"
	"net/http"
)

type HealthController struct {
	Checks []NamedCheck
}

type result struct {
	Index int
	Check NamedCheck
	Err   error
}

func (ctrl *HealthController) Check(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	results := make(chan result, len(ctrl.Checks))
	for index, ch := range ctrl.Checks {
		go func(index int, ch NamedCheck) {
			results <- result{
				Index: index,
				Check: ch,
				Err:   ch.Do(ctx),
			}
		}(index, ch)
	}

	response := map[string]string{}
	completed := make([]bool, len(ctrl.Checks))
	hasError := false

	recordResult := func(r result) {
		completed[r.Index] = true
		if r.Err != nil {
			hasError = true
			response[r.Check.Name()] = r.Err.Error()
		} else {
			response[r.Check.Name()] = "OK"
		}
	}

	completedCount := 0
	for completedCount < len(ctrl.Checks) {
		select {
		case r := <-results:
			recordResult(r)
			completedCount++
		case <-ctx.Done():
			for completedCount < len(ctrl.Checks) {
				select {
				case r := <-results:
					recordResult(r)
					completedCount++
				default:
					hasError = true
					for index, ch := range ctrl.Checks {
						if !completed[index] {
							response[ch.Name()] = ctx.Err().Error()
						}
					}
					writeResponse(w, response, hasError)
					return
				}
			}
		}
	}

	writeResponse(w, response, hasError)
}

func writeResponse(w http.ResponseWriter, response map[string]string, hasError bool) {
	if hasError {
		w.WriteHeader(http.StatusInternalServerError)
	} else {
		w.WriteHeader(http.StatusOK)
	}

	err := json.NewEncoder(w).Encode(response)
	if err != nil {
		panic(err)
	}
}

func NewHealthController(checks ...NamedCheck) *HealthController {
	return &HealthController{
		Checks: checks,
	}
}
