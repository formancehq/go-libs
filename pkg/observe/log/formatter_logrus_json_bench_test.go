package logging

import (
	"io"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func benchEntry(fields logrus.Fields) *logrus.Entry {
	l := logrus.New()
	l.SetOutput(io.Discard)

	return l.WithFields(fields).WithTime(time.Now())
}

func benchFormatter(b *testing.B, f logrus.Formatter, fields logrus.Fields) {
	b.Helper()

	entry := benchEntry(fields)
	entry.Level = logrus.InfoLevel
	entry.Message = "batch applied"

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		if _, err := f.Format(entry); err != nil {
			b.Fatal(err)
		}
	}
}

var benchFields = logrus.Fields{
	"source": "bitcoin-regtest", "requests": 2858, "postings": 14311,
	"latency": 323704112, "has_more": true,
}

func BenchmarkLogrusJSONFormatter_NoFields(b *testing.B) {
	benchFormatter(b, &logrus.JSONFormatter{}, nil)
}

func BenchmarkSharedJSONFormatter_NoFields(b *testing.B) {
	benchFormatter(b, &sharedJSONFormatter{}, nil)
}

func BenchmarkLogrusJSONFormatter_FiveFields(b *testing.B) {
	benchFormatter(b, &logrus.JSONFormatter{}, benchFields)
}

func BenchmarkSharedJSONFormatter_FiveFields(b *testing.B) {
	benchFormatter(b, &sharedJSONFormatter{}, benchFields)
}
