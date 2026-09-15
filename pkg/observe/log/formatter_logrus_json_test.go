package logging

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"go.uber.org/zap/zapcore"
)

func logrusJSONRecord(t *testing.T, level Level, emit func(Logger)) (string, map[string]any) {
	t.Helper()

	var buf bytes.Buffer
	emit(NewDefaultLoggerWithLevel(&buf, level, true, false))

	line := strings.TrimRight(buf.String(), "\n")

	var record map[string]any
	if err := json.Unmarshal([]byte(line), &record); err != nil {
		t.Fatalf("output is not valid JSON: %q (%v)", line, err)
	}

	return line, record
}

// The whole point of aligning the formatter: a service on the logrus stack and
// a service on the zap stack must emit the same keys, in the same order, with
// the same level spelling.
func TestLogrusJSONMatchesTheZapRecordShape(t *testing.T) {
	logrusLine, _ := logrusJSONRecord(t, InfoLevel, func(l Logger) { l.Infof("listening") })

	var zapBuf bytes.Buffer
	NewSlog(NewZapLogger(&zapBuf, zapcore.InfoLevel, true)).Info("listening")
	zapLine := strings.TrimRight(zapBuf.String(), "\n")

	if keyOrder(t, logrusLine) != keyOrder(t, zapLine) {
		t.Fatalf("key order differs:\n  logrus: %s\n  zap:    %s", logrusLine, zapLine)
	}
}

// keyOrder returns the keys of a JSON object in the order they appear in the
// encoded text, which is what a reader scanning a log stream actually sees.
func keyOrder(t *testing.T, line string) string {
	t.Helper()

	dec := json.NewDecoder(strings.NewReader(line))
	if _, err := dec.Token(); err != nil { // opening brace
		t.Fatalf("not a JSON object: %q", line)
	}

	var keys []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			t.Fatalf("decoding %q: %v", line, err)
		}
		keys = append(keys, tok.(string))

		var discard any
		if err := dec.Decode(&discard); err != nil {
			t.Fatalf("decoding %q: %v", line, err)
		}
	}

	return strings.Join(keys, ",")
}

func TestLogrusJSONCapitalisesLevels(t *testing.T) {
	for _, tc := range []struct {
		level Level
		emit  func(Logger)
		want  string
	}{
		{InfoLevel, func(l Logger) { l.Infof("x") }, "INFO"},
		{DebugLevel, func(l Logger) { l.Debugf("x") }, "DEBUG"},
		{TraceLevel, func(l Logger) { l.Tracef("x") }, "TRACE"},
		{ErrorLevel, func(l Logger) { l.Errorf("x") }, "ERROR"},
	} {
		_, record := logrusJSONRecord(t, tc.level, tc.emit)
		if record["level"] != tc.want {
			t.Fatalf("level = %v, want %v", record["level"], tc.want)
		}
	}
}

// logrus spells this level "warning"; the shared shape spells it "warn", and
// capitalising logrus's own name would have produced "WARNING".
func TestLogrusJSONSpellsWarnLikeZap(t *testing.T) {
	var buf bytes.Buffer
	l := logrus.New()
	l.SetOutput(&buf)
	l.SetLevel(logrus.WarnLevel)
	l.SetFormatter(&sharedJSONFormatter{})
	l.Warn("close to the limit")

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("not JSON: %s", buf.String())
	}
	if record["level"] != "WARN" {
		t.Fatalf("level = %v, want WARN", record["level"])
	}
}

func TestLogrusJSONTimestampHasNanosecondPrecision(t *testing.T) {
	_, record := logrusJSONRecord(t, InfoLevel, func(l Logger) { l.Infof("x") })

	ts, ok := record["time"].(string)
	if !ok {
		t.Fatalf("no string \"time\" field: %v", record)
	}
	if _, err := time.Parse(time.RFC3339Nano, ts); err != nil {
		t.Fatalf("time must parse as RFC3339Nano, got %q: %v", ts, err)
	}
	if !strings.Contains(ts, ".") {
		t.Fatalf("second-precision timestamps lose ordering within a second: %q", ts)
	}
}

func TestLogrusJSONCarriesFieldsSorted(t *testing.T) {
	line, record := logrusJSONRecord(t, InfoLevel, func(l Logger) {
		l.WithFields(map[string]any{"status": 200, "addr": ":8080"}).Infof("Request")
	})

	if record["addr"] != ":8080" || record["status"] != float64(200) {
		t.Fatalf("fields missing: %v", record)
	}
	if got := keyOrder(t, line); got != "level,time,msg,addr,status" {
		t.Fatalf("key order = %q", got)
	}
}

// encoding/json renders an error as {}, and the field carrying one is usually
// the field a reader most needs.
func TestLogrusJSONRendersErrorsAsMessages(t *testing.T) {
	_, record := logrusJSONRecord(t, InfoLevel, func(l Logger) {
		l.WithField("error", errors.New("ledger down")).Infof("apply failed")
	})

	if record["error"] != "ledger down" {
		t.Fatalf("error field = %v, want its message", record["error"])
	}
}

// A value that cannot be marshalled must cost that field's fidelity, not the
// whole record. +Inf is the case to use here: encoding/json refuses it, while
// logrus accepts it as a field (it rejects only func-typed values itself).
func TestLogrusJSONSurvivesUnmarshalableValues(t *testing.T) {
	_, record := logrusJSONRecord(t, InfoLevel, func(l Logger) {
		l.WithField("ratio", math.Inf(1)).Infof("still logged")
	})

	if record["msg"] != "still logged" {
		t.Fatalf("record lost: %v", record)
	}
	if _, ok := record["ratio"].(string); !ok {
		t.Fatalf("unmarshalable field should degrade to a string: %v", record)
	}
}

// Regression for a review finding: a field named like one of the three keys
// the formatter writes itself would otherwise produce a duplicate key, and a
// consumer keeping the last value would lose the real message, level or
// timestamp. logrus.JSONFormatter renamed them the same way.
func TestLogrusJSONRenamesReservedFieldCollisions(t *testing.T) {
	line, record := logrusJSONRecord(t, InfoLevel, func(l Logger) {
		l.WithFields(map[string]any{
			"msg":   "user supplied",
			"level": "user level",
			"time":  "user time",
		}).Infof("the real message")
	})

	if record["msg"] != "the real message" {
		t.Fatalf("the record's own message must win: %v", record)
	}
	if record["level"] != "INFO" {
		t.Fatalf("the record's own level must win: %v", record)
	}
	if record["fields.msg"] != "user supplied" ||
		record["fields.level"] != "user level" ||
		record["fields.time"] != "user time" {
		t.Fatalf("colliding fields must survive under fields.*: %v", record)
	}

	if got := keyOrder(t, line); got != "level,time,msg,fields.level,fields.msg,fields.time" {
		t.Fatalf("key order = %q", got)
	}
}

// A non-colliding field keeps its own name.
func TestLogrusJSONLeavesOrdinaryFieldNamesAlone(t *testing.T) {
	_, record := logrusJSONRecord(t, InfoLevel, func(l Logger) {
		l.WithField("message", "not reserved").Infof("x")
	})

	if record["message"] != "not reserved" {
		t.Fatalf("ordinary field renamed: %v", record)
	}
}
