package logging

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"go.uber.org/zap/zapcore"
)

// sharedLogrus builds a logrus logger on one of the shared formatters.
func sharedLogrus(buf *bytes.Buffer, level Level, formatter logrus.Formatter) Logger {
	l := logrus.New()
	l.SetOutput(buf)
	l.SetLevel(toLogrusLevel(level))
	l.SetFormatter(formatter)

	return NewLogrus(l)
}

var timestamp = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T[0-9:.+\-Z]+`)

// normalise removes the one thing two runs cannot share.
func normalise(s string) string {
	return timestamp.ReplaceAllString(strings.TrimRight(s, "\n"), "T")
}

// The guarantee the shared formatters make: a logrus service and a zap service
// emit the same bytes, not merely a similar shape. Asserting on the rendered
// line is what makes a drift in key order, level spelling or timestamp
// precision impossible to miss.
func TestSharedFormattersMatchTheZapStackByteForByte(t *testing.T) {
	for _, tc := range []struct {
		name      string
		formatter logrus.Formatter
		json      bool
	}{
		{"json", NewSharedJSONFormatter(), true},
		{"text", NewSharedTextFormatter(), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var fromLogrus, fromZap bytes.Buffer

			sharedLogrus(&fromLogrus, InfoLevel, tc.formatter).
				WithFields(map[string]any{"addr": ":8080", "auth": true}).Infof("listening")

			NewSlog(NewZapLogger(&fromZap, zapcore.InfoLevel, tc.json)).
				Info("listening", "addr", ":8080", "auth", true)

			if normalise(fromLogrus.String()) != normalise(fromZap.String()) {
				t.Fatalf("the two stacks diverge:\n logrus: %s\n    zap: %s", fromLogrus.String(), fromZap.String())
			}
		})
	}
}

// Levels are where the two stacks used to disagree most.
func TestSharedFormattersMatchTheZapStackAtEveryLevel(t *testing.T) {
	for _, tc := range []struct {
		level Level
		emit  func(Logger)
		zap   func(*bytes.Buffer)
	}{
		{DebugLevel, func(l Logger) { l.Debugf("x") }, func(b *bytes.Buffer) {
			NewSlog(NewZapLogger(b, zapcore.DebugLevel, true)).Debug("x")
		}},
		{InfoLevel, func(l Logger) { l.Infof("x") }, func(b *bytes.Buffer) {
			NewSlog(NewZapLogger(b, zapcore.InfoLevel, true)).Info("x")
		}},
		{ErrorLevel, func(l Logger) { l.Errorf("x") }, func(b *bytes.Buffer) {
			NewSlog(NewZapLogger(b, zapcore.ErrorLevel, true)).Error("x")
		}},
	} {
		var fromLogrus, fromZap bytes.Buffer
		tc.emit(sharedLogrus(&fromLogrus, tc.level, NewSharedJSONFormatter()))
		tc.zap(&fromZap)

		if normalise(fromLogrus.String()) != normalise(fromZap.String()) {
			t.Fatalf("level %v diverges:\n logrus: %s\n    zap: %s", tc.level, fromLogrus.String(), fromZap.String())
		}
	}
}

// logrus spells this level "warning" and has no Level for it; the shared shape
// follows zap.
func TestSharedFormatterSpellsWarnLikeZap(t *testing.T) {
	var buf bytes.Buffer
	l := logrus.New()
	l.SetOutput(&buf)
	l.SetLevel(logrus.WarnLevel)
	l.SetFormatter(NewSharedJSONFormatter())
	l.Warn("close to the limit")

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("not JSON: %s", buf.String())
	}
	if record["level"] != "WARN" {
		t.Fatalf("level = %v, want WARN", record["level"])
	}
}

// A field colliding with a key the encoder writes itself would otherwise
// shadow the record's own value.
func TestSharedFormatterRenamesReservedFieldCollisions(t *testing.T) {
	var buf bytes.Buffer
	sharedLogrus(&buf, InfoLevel, NewSharedJSONFormatter()).
		WithFields(map[string]any{"msg": "user supplied", "level": "user level"}).
		Infof("the real message")

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("not JSON: %s", buf.String())
	}
	if record["msg"] != "the real message" || record["level"] != "INFO" {
		t.Fatalf("the record's own values must win: %v", record)
	}
	if record["fields.msg"] != "user supplied" || record["fields.level"] != "user level" {
		t.Fatalf("colliding fields must survive under fields.*: %v", record)
	}
}

// Renaming must not depend on Go's map iteration order.
func TestSharedFormatterResolvesRenamedCollisionsDeterministically(t *testing.T) {
	for range 200 {
		var buf bytes.Buffer
		sharedLogrus(&buf, InfoLevel, NewSharedJSONFormatter()).
			WithFields(map[string]any{"msg": "reserved", "fields.msg": "literal"}).
			Infof("the real message")

		var record map[string]any
		if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
			t.Fatalf("not JSON: %s", buf.String())
		}
		if record["fields.msg"] != "reserved" {
			t.Fatalf("fields.msg = %v, want the renamed reserved field to win every time", record["fields.msg"])
		}
	}
}

// An error field renders as its message in both stacks, not as {}.
func TestSharedFormatterRendersErrorsAsMessages(t *testing.T) {
	var buf bytes.Buffer
	sharedLogrus(&buf, InfoLevel, NewSharedJSONFormatter()).
		WithField("error", errors.New("ledger down")).Infof("apply failed")

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("not JSON: %s", buf.String())
	}
	if record["error"] != "ledger down" {
		t.Fatalf("error = %v, want its message", record["error"])
	}
}

func TestSharedFormatterKeepsDurationsAsNanoseconds(t *testing.T) {
	var buf bytes.Buffer
	sharedLogrus(&buf, InfoLevel, NewSharedJSONFormatter()).
		WithField("latency", 250*time.Millisecond).Infof("applied")

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("not JSON: %s", buf.String())
	}
	if record["latency"] != float64(250000000) {
		t.Fatalf("latency = %v", record["latency"])
	}
}

// The guarantee to every service that has not migrated: NewDefaultLogger's
// output is what logrus always produced, in both formats.
func TestNewDefaultLoggerKeepsLogrusFormats(t *testing.T) {
	for _, jsonFormat := range []bool{true, false} {
		var ours, theirs bytes.Buffer

		NewDefaultLoggerWithLevel(&ours, InfoLevel, jsonFormat, false).
			WithField("addr", ":8080").Infof("listening")

		reference := logrus.New()
		reference.SetOutput(&theirs)
		reference.SetLevel(logrus.InfoLevel)
		if jsonFormat {
			reference.SetFormatter(&logrus.JSONFormatter{})
		} else {
			text := new(logrus.TextFormatter)
			text.FullTimestamp = true
			reference.SetFormatter(text)
		}
		reference.WithField("addr", ":8080").Infof("listening")

		if normalise(ours.String()) != normalise(theirs.String()) {
			t.Fatalf("json=%v: the default logger diverged from logrus:\n ours: %s\n them: %s",
				jsonFormat, ours.String(), theirs.String())
		}
	}
}
