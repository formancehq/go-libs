package logging

import (
	"sort"

	"github.com/sirupsen/logrus"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// NewSharedJSONFormatter and NewSharedTextFormatter return logrus formatters
// rendering entries exactly as the zap stack does, for a service that has to
// stay on logrus but wants its records to line up with the rest of a
// deployment.
//
// Both are opt-in: NewDefaultLogger keeps logrus's own formatters, because
// changing them would rewrite the output of every service that has not
// migrated. Use one on a logger you build yourself:
//
//	l := logrus.New()
//	l.SetFormatter(logging.NewSharedJSONFormatter())
//	logger := logging.NewLogrus(l)
//
// The migration each implies is described in docs/LOGGING.md.
func NewSharedJSONFormatter() logrus.Formatter {
	return &sharedFormatter{encoder: zapcore.NewJSONEncoder(ZapEncoderConfig())}
}

// NewSharedTextFormatter is the console counterpart, matching what
// NewZapLogger writes when jsonFormatting is unset.
func NewSharedTextFormatter() logrus.Formatter {
	return &sharedFormatter{encoder: zapcore.NewConsoleEncoder(ZapEncoderConfig())}
}

// sharedFormatter renders a logrus entry through one of zap's own encoders.
//
// Delegating rather than reproducing the layout is the whole point: the two
// stacks cannot drift apart, because there is only one encoder. An earlier
// revision wrote the JSON by hand and had to be corrected three times -- key
// order, level spelling, timestamp precision -- each a divergence this
// approach cannot express.
//
// zap's encoders clone their state inside EncodeEntry, so one encoder is safe
// to share across goroutines.
type sharedFormatter struct {
	encoder zapcore.Encoder
}

var _ logrus.Formatter = (*sharedFormatter)(nil)

func (f *sharedFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	buf, err := f.encoder.EncodeEntry(zapcore.Entry{
		Level:   logrusToZapLevel(entry.Level),
		Time:    entry.Time,
		Message: entry.Message,
	}, entryFields(entry))
	if err != nil {
		return nil, err
	}
	defer buf.Free()

	// logrus keeps the slice; zap's buffer goes back to its pool.
	out := make([]byte, buf.Len())
	copy(out, buf.Bytes())

	return out, nil
}

// entryFields converts logrus's field map into zap fields, renaming the ones
// that would collide with a key the encoder writes itself and ordering them so
// a record renders the same way twice.
// sharedEscapableKeys is every key the zap stack escapes: the encoder's own,
// plus the correlation ids. Built once -- entryFields runs on every record
// carrying a field.
var sharedEscapableKeys = append(append([]string{}, reservedKeys[:]...), "trace_id", "span_id")

func entryFields(entry *logrus.Entry) []zapcore.Field {
	if len(entry.Data) == 0 {
		return nil
	}

	// Copy first, rename second. Renaming while copying would make two keys --
	// "msg" and a literal "fields.msg" -- land on the same destination, and
	// Go's randomised map iteration would decide which value survived.
	data := make(map[string]any, len(entry.Data))
	for k, v := range entry.Data {
		data[k] = v
	}

	// The same set the zap stack escapes -- the reserved keys and the
	// correlation ids -- or a field's path changes when a service moves from
	// one stack to the other, which is the whole property these formatters
	// exist to provide.
	for _, reserved := range sharedEscapableKeys {
		if v, ok := data[reserved]; ok {
			data[escapeScoped(reserved)] = v
			delete(data, reserved)
		}
	}

	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	// logrus has already lost the insertion order, so sorting is what makes a
	// record's rendering stable rather than following map iteration.
	sort.Strings(keys)

	fields := make([]zapcore.Field, 0, len(keys))
	for _, k := range keys {
		// zap.Any maps an error to its message, a Stringer to its String, and
		// anything else to the encoding/json rendering -- the semantics the
		// zap stack already applies to the same value.
		fields = append(fields, zap.Any(k, data[k]))
	}

	return fields
}

// logrusToZapLevel maps a logrus level onto the zapcore level whose rendering
// ZapEncoderConfig defines. logrus spells its warning level "warning"; zap
// spells it "warn", and the shared shape follows zap.
func logrusToZapLevel(level logrus.Level) zapcore.Level {
	switch level {
	case logrus.TraceLevel:
		return ToZapLevel(TraceLevel)
	case logrus.DebugLevel:
		return zapcore.DebugLevel
	case logrus.InfoLevel:
		return zapcore.InfoLevel
	case logrus.WarnLevel:
		return zapcore.WarnLevel
	case logrus.ErrorLevel:
		return zapcore.ErrorLevel
	case logrus.FatalLevel:
		return zapcore.FatalLevel
	case logrus.PanicLevel:
		return zapcore.PanicLevel
	default:
		return zapcore.InfoLevel
	}
}
