package logging

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

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
//
// It encodes strings and the common scalar types itself rather than calling
// json.Marshal per field. That is not premature: a formatter runs on the hot
// path of every service that logs per event, and reflection-based encoding of
// five fields costs more allocations than the record itself. json.Marshal
// remains the fallback for anything else, so correctness never depends on the
// fast path covering a type.
type sharedJSONFormatter struct{}

var _ logrus.Formatter = (*sharedJSONFormatter)(nil)

// bufferPool amortises the per-record buffer. Format has to return a []byte
// logrus owns, so the pooled buffer is copied out and returned to the pool
// rather than handed over.
var bufferPool = sync.Pool{
	New: func() any {
		return bytes.NewBuffer(make([]byte, 0, 512))
	},
}

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

// hasReservedField reports whether any field would be renamed. Three map
// lookups keep the renaming off the path every ordinary record takes.
func hasReservedField(data logrus.Fields) bool {
	for _, k := range [...]string{"level", "time", "msg"} {
		if _, ok := data[k]; ok {
			return true
		}
	}

	return false
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
	buf, _ := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufferPool.Put(buf)

	buf.WriteByte('{')

	writeJSONString(buf, "level")
	buf.WriteByte(':')
	writeJSONString(buf, levelName(entry.Level))

	buf.WriteString(`,"time":`)
	// AppendFormat writes into the buffer's own storage instead of allocating
	// the formatted timestamp as a string first.
	buf.WriteByte('"')
	buf.Write(entry.Time.AppendFormat(buf.AvailableBuffer(), time.RFC3339Nano))
	buf.WriteByte('"')

	buf.WriteString(`,"msg":`)
	writeJSONString(buf, entry.Message)

	if len(entry.Data) > 0 {
		// logrus holds the entry's fields in a map, so their insertion order is
		// already lost by the time a formatter sees them. Sorting keeps a
		// record's rendering stable across runs instead of following Go's map
		// iteration order.
		keys := make([]string, 0, len(entry.Data))
		for k := range entry.Data {
			keys = append(keys, emittedFieldName(k))
		}
		sort.Strings(keys)

		// A colliding field is rare, so the common path reads the value back
		// under the key it was stored with and never pays for the renaming.
		renamed := hasReservedField(entry.Data)

		for _, emitted := range keys {
			buf.WriteByte(',')
			writeJSONString(buf, emitted)
			buf.WriteByte(':')

			key := emitted
			if renamed {
				key = strings.TrimPrefix(emitted, "fields.")
				if _, ok := entry.Data[emitted]; ok {
					key = emitted
				}
			}

			if err := writeJSONValue(buf, entry.Data[key]); err != nil {
				return nil, fmt.Errorf("encode log field %q: %w", key, err)
			}
		}
	}

	buf.WriteString("}\n")

	// logrus keeps the returned slice, so it cannot alias the pooled buffer.
	out := make([]byte, buf.Len())
	copy(out, buf.Bytes())

	return out, nil
}

// writeJSONValue encodes the types a log field actually carries without
// reflection, and falls back to json.Marshal for everything else.
func writeJSONValue(buf *bytes.Buffer, value any) error {
	switch v := value.(type) {
	case nil:
		buf.WriteString("null")
	case string:
		writeJSONString(buf, v)
	case bool:
		if v {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case int:
		buf.Write(strconv.AppendInt(buf.AvailableBuffer(), int64(v), 10))
	case int8:
		buf.Write(strconv.AppendInt(buf.AvailableBuffer(), int64(v), 10))
	case int16:
		buf.Write(strconv.AppendInt(buf.AvailableBuffer(), int64(v), 10))
	case int32:
		buf.Write(strconv.AppendInt(buf.AvailableBuffer(), int64(v), 10))
	case int64:
		buf.Write(strconv.AppendInt(buf.AvailableBuffer(), v, 10))
	case uint:
		buf.Write(strconv.AppendUint(buf.AvailableBuffer(), uint64(v), 10))
	case uint8:
		buf.Write(strconv.AppendUint(buf.AvailableBuffer(), uint64(v), 10))
	case uint16:
		buf.Write(strconv.AppendUint(buf.AvailableBuffer(), uint64(v), 10))
	case uint32:
		buf.Write(strconv.AppendUint(buf.AvailableBuffer(), uint64(v), 10))
	case uint64:
		buf.Write(strconv.AppendUint(buf.AvailableBuffer(), v, 10))
	case time.Duration:
		buf.Write(strconv.AppendInt(buf.AvailableBuffer(), int64(v), 10))
	case error:
		// encoding/json renders an error as {}, and the field carrying one is
		// usually the field a reader most needs. logrus.JSONFormatter makes the
		// same substitution.
		writeJSONString(buf, v.Error())
	case fmt.Stringer:
		writeJSONString(buf, v.String())
	default:
		return writeJSONFallback(buf, value)
	}

	return nil
}

func writeJSONFallback(buf *bytes.Buffer, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		// A value encoding/json refuses -- a float infinity, a cyclic
		// structure -- must cost that field's fidelity, not the whole record.
		writeJSONString(buf, fmt.Sprintf("%v", value))

		return nil
	}

	buf.Write(encoded)

	return nil
}

const hexDigits = "0123456789abcdef"

// writeJSONString writes s as a quoted JSON string. It escapes exactly what
// RFC 8259 requires -- quote, backslash and the control characters -- and
// leaves <, > and & alone, matching zap's encoder rather than
// encoding/json's HTML-safe default, so the two stacks render a URL the same
// way.
func writeJSONString(buf *bytes.Buffer, s string) {
	buf.WriteByte('"')

	start := 0
	for i := 0; i < len(s); {
		if b := s[i]; b < utf8.RuneSelf {
			if b >= 0x20 && b != '"' && b != '\\' {
				i++

				continue
			}

			buf.WriteString(s[start:i])

			switch b {
			case '"':
				buf.WriteString(`\"`)
			case '\\':
				buf.WriteString(`\\`)
			case '\n':
				buf.WriteString(`\n`)
			case '\r':
				buf.WriteString(`\r`)
			case '\t':
				buf.WriteString(`\t`)
			default:
				buf.WriteString(`\u00`)
				buf.WriteByte(hexDigits[b>>4])
				buf.WriteByte(hexDigits[b&0xF])
			}

			i++
			start = i

			continue
		}

		// Invalid UTF-8 would produce a string no JSON decoder accepts, so it
		// is replaced rather than passed through.
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			buf.WriteString(s[start:i])
			buf.WriteString(`\ufffd`)
			i += size
			start = i

			continue
		}

		i += size
	}

	buf.WriteString(s[start:])
	buf.WriteByte('"')
}
