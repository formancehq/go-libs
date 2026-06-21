package temporal

import (
	"fmt"

	"go.temporal.io/sdk/log"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
)

func keyvalsToMap(keyvals ...interface{}) map[string]any {
	ret := make(map[string]any)
	for i := 0; i+1 < len(keyvals); i += 2 {
		ret[fmt.Sprint(keyvals[i])] = keyvals[i+1]
	}
	return ret
}

type logger struct {
	logger logging.Logger
}

type warnLogger interface {
	Warnf(format string, args ...any)
}

func (l logger) Debug(msg string, keyvals ...interface{}) {
	l.logger.WithFields(keyvalsToMap(keyvals...)).Debugf("%s", msg)
}

func (l logger) Info(msg string, keyvals ...interface{}) {
	l.logger.WithFields(keyvalsToMap(keyvals...)).Infof("%s", msg)
}

func (l logger) Warn(msg string, keyvals ...interface{}) {
	logger := l.logger.WithFields(keyvalsToMap(keyvals...))
	if warnLogger, ok := logger.(warnLogger); ok {
		warnLogger.Warnf("%s", msg)
		return
	}
	logger.Infof("%s", msg)
}

func (l logger) Error(msg string, keyvals ...interface{}) {
	l.logger.WithFields(keyvalsToMap(keyvals...)).Errorf("%s", msg)
}

var _ log.Logger = (*logger)(nil)

func newLogger(l logging.Logger) *logger {
	return &logger{
		logger: l,
	}
}
