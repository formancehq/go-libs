package websocketclient

import (
	"encoding/json"
)

// DefaultQueueBytes caps retained encoded payload per queue, in addition to its
// item bound. It does not claim to measure total Go heap or provider caches.
const DefaultQueueBytes = 32 << 20

// MaxBuffered is the default keyed-buffer item capacity.
const MaxBuffered = 50000

// Scalar queue signals avoid serialization allocations. The charge is a
// conservative encoded-size bound; structured values use their JSON size.
func encodedSize(value any) (int, error) {
	switch v := value.(type) {
	case nil:
		return 4, nil
	case bool:
		return 5, nil
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return 32, nil
	case string:
		if len(v) > DefaultQueueBytes {
			return DefaultQueueBytes + 1, nil
		}
		return len(v)*6 + 2, nil
	case []byte:
		return 4*((len(v)+2)/3) + 2, nil
	case json.RawMessage:
		return len(v), nil
	default:
		raw, err := json.Marshal(value)
		return len(raw), err
	}
}
