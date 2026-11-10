package audit

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPayloadOmitsGRPCForHTTP(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(Payload{ID: "payload-id"})
	require.NoError(t, err)

	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &fields))
	assert.NotContains(t, fields, "grpc")
}

func TestPayloadGRPCWireFormat(t *testing.T) {
	t.Parallel()

	code := 7
	payload := Payload{
		ID: "payload-id",
		GRPC: &GRPC{
			Service:       "example.Accounts",
			Method:        "Read",
			StatusCode:    &code,
			StatusMessage: "permission denied",
			Termination:   GRPCTerminationCompleted,
			Request: GRPCBody{
				Encoding:   GRPCBodyEncodingBase64,
				Data:       []byte{0, 0, 0, 0, 0},
				BytesTotal: 5,
				Complete:   true,
			},
			Response: GRPCBody{
				Encoding: GRPCBodyEncodingBase64,
				Complete: true,
			},
		},
	}

	raw, err := json.Marshal(payload)
	require.NoError(t, err)

	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &fields))
	assert.JSONEq(t, `{
		"service": "example.Accounts",
		"method": "Read",
		"status_code": 7,
		"status_message": "permission denied",
		"termination": "completed",
		"request": {"encoding": "base64", "data": "AAAAAAA=", "bytes_total": 5, "truncated": false, "complete": true},
		"response": {"encoding": "base64", "bytes_total": 0, "truncated": false, "complete": true}
	}`, string(fields["grpc"]))

	var decoded Payload
	require.NoError(t, json.Unmarshal(raw, &decoded))
	assert.Equal(t, payload, decoded)
}

func TestPayloadGRPCOmitsAbsentStatus(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(GRPC{Termination: GRPCTerminationIncomplete})
	require.NoError(t, err)

	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &fields))
	assert.NotContains(t, fields, "status_code")
	assert.NotContains(t, fields, "status_message")
	assert.NotContains(t, fields, "gateway_status_code")
}
