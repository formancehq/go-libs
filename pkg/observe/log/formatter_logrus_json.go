package logging

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/sirupsen/logrus"
)

// sharedJSONFormatter renders a logrus entry in the same shape as
// ZapEncoderConfig, so a deployment running services on both logger stacks
// emits one record shape rather than two.
//
// It replaces logrus.JSONFormatter rather than configuring it, for three
// reasons that formatter cannot address: it lowercases the level where the
// shared shape capitalises it, it spells the warning level "warning" where zap
// spells it "warn", and it marshals a map, so Go sorts the keys alphabetically
// and `msg` lands before `time`. Writing the fields directly fixes all three
// and pins the order to zap's: level, time, msg, then the entry's own fields.
type sharedJSONFormatter struct{}

var _ logrus.Formatter = (*sharedJSONFormatter)(nil)

// levelName maps a logrus level onto the spelling ZapEncoderConfig emits.
// Only the levels logging.Level can express are remapped; the rest keep their
// logrus name, capitalised, so a Fatal or Panic record is still readable.
func levelName(level logrus.Level) string {
	switch level {
	case logrus.TraceLevel:
		return "TRACE"
	case logrus.DebugLevel:
		return "DEBUG"
	case logrus.InfoLevel:
		return "INFO"
	case logrus.WarnLevel:
		// logrus spells this level "warning"; zap spells it "warn".
		return "WARN"
	case logrus.ErrorLevel:
		return "ERROR"
	case logrus.FatalLevel:
		return "FATAL"
	case logrus.PanicLevel:
		return "PANIC"
	default:
		return "INFO"
	}
}

func (f *sharedJSONFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')

	if err := writeJSONField(&buf, true, "level", levelName(entry.Level)); err != nil {
		return nil, err
	}
	if err := writeJSONField(&buf, false, "time", entry.Time.Format(time.RFC3339Nano)); err != nil {
		return nil, err
	}
	if err := writeJSONField(&buf, false, "msg", entry.Message); err != nil {
		return nil, err
	}

	// logrus holds the entry's fields in a map, so their insertion order is
	// already lost by the time a formatter sees them. Sorting keeps a record's
	// rendering stable across runs instead of following Go's map iteration.
	keys := make([]string, 0, len(entry.Data))
	for k := range entry.Data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		if err := writeJSONField(&buf, false, k, entry.Data[k]); err != nil {
			return nil, err
		}
	}

	buf.WriteString("}\n")

	return buf.Bytes(), nil
}

func writeJSONField(buf *bytes.Buffer, first bool, key string, value any) error {
	if !first {
		buf.WriteByte(',')
	}

	encoded, err := json.Marshal(key)
	if err != nil {
		return fmt.Errorf("encode log field name %q: %w", key, err)
	}

	buf.Write(encoded)
	buf.WriteByte(':')

	// An error value does not marshal to anything useful -- encoding/json
	// renders it as {} -- and a field carrying one is usually the field a
	// reader most needs. Render it as its message, as logrus.JSONFormatter
	// does.
	if err, ok := value.(error); ok {
		value = err.Error()
	}

	encoded, err = json.Marshal(value)
	if err != nil {
		// A value that cannot be marshalled must not cost the whole record.
		encoded, err = json.Marshal(fmt.Sprintf("%v", value))
		if err != nil {
			return fmt.Errorf("encode log field %q: %w", key, err)
		}
	}

	buf.Write(encoded)

	return nil
}
