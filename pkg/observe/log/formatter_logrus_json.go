package logging

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/sirupsen/logrus"
)

// NewSharedJSONFormatter returns a logrus formatter rendering entries in the
// same shape as ZapEncoderConfig, for a service that has to stay on logrus but
// wants its records to line up with the zap stack's.
//
// It is opt-in: NewDefaultLogger keeps logrus's own shape, because changing
// that would rewrite the records of every service that has not migrated. Use
// it on a logger you build yourself:
//
//	l := logrus.New()
//	l.SetFormatter(logging.NewSharedJSONFormatter())
//	logger := logging.NewLogrus(l)
//
// The migration it implies is described in docs/LOGGING.md.
func NewSharedJSONFormatter() logrus.Formatter {
	return &sharedJSONFormatter{}
}

// sharedJSONFormatter renders a logrus entry in the same shape as
// ZapEncoderConfig, so a deployment running services on both logger stacks
// emits one record shape rather than two.
//
// It replaces logrus.JSONFormatter rather than configuring it, for three
// reasons that formatter cannot address: it lowercases the level where the
// shared shape capitalises it, it spells the warning level "warning" where zap
// spells it "warn", and it marshals a map, so encoding/json sorts the keys
// alphabetically and `msg` lands before `time`. Writing the fields in order
// fixes all three.
//
// Values still go through encoding/json, which keeps the semantics every
// existing consumer already relies on -- a time.Time stays RFC 3339, a
// json.Number stays a number, a json.Marshaler keeps its own representation.
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

// emittedFieldName renames a field that would collide with one of the three
// keys this formatter writes itself. Without it a caller attaching a field
// named "msg" produces a record with two "msg" keys, and a consumer keeping
// the last value loses the actual message -- or the severity, or the
// timestamp. logrus.JSONFormatter made the same substitution, so a record that
// used to read "fields.msg" still does.
func emittedFieldName(key string) string {
	switch key {
	case "level", "time", "msg":
		return "fields." + key
	default:
		return key
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

	// Copy first, rename second. Renaming while copying would make two keys --
	// "msg" and a literal "fields.msg" -- land on the same destination, and Go's
	// randomised map iteration would decide which value survived. Applying the
	// renames afterwards gives the reserved field precedence every time, which
	// is what logrus.prefixFieldClashes did.
	fields := make(map[string]any, len(entry.Data))
	for k, v := range entry.Data {
		fields[k] = v
	}

	for _, reserved := range [...]string{"level", "time", "msg"} {
		if v, ok := fields[reserved]; ok {
			fields[emittedFieldName(reserved)] = v
			delete(fields, reserved)
		}
	}

	// logrus holds the entry's fields in a map, so their insertion order is
	// already lost by the time a formatter sees them. Sorting keeps a record's
	// rendering stable across runs instead of following Go's map iteration.
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		if err := writeJSONField(&buf, false, k, fields[k]); err != nil {
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

	encodedKey, err := json.Marshal(key)
	if err != nil {
		return fmt.Errorf("encode log field name %q: %w", key, err)
	}

	buf.Write(encodedKey)
	buf.WriteByte(':')

	// An error does not marshal to anything useful -- encoding/json renders it
	// as {} -- and a field carrying one is usually the field a reader most
	// needs. logrus.JSONFormatter makes the same substitution.
	if e, ok := value.(error); ok {
		value = e.Error()
	}

	encoded, err := json.Marshal(value)
	if err != nil {
		// The value must cost its own fidelity, not the whole record. The
		// diagnostic is the marshal error rather than the value: encoding/json
		// rejects a cyclic structure, and fmt would traverse that same cycle
		// without detection and take the process down with a stack overflow.
		encoded, err = json.Marshal(fmt.Sprintf("<unencodable %T: %s>", value, err))
		if err != nil {
			return fmt.Errorf("encode log field %q: %w", key, err)
		}
	}

	buf.Write(encoded)

	return nil
}
