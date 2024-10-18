package logging

import (
	"context"
	"fmt"
	"io"

	"github.com/hashicorp/go-hclog"
)

// Logger implements logging.Logger with *hclog.Logger.
type HcLogLoggerAdapter struct {
	ctx     context.Context
	backend hclog.Logger

	hooks []string
}

// NewLogger returns new logging.Logger using passed *hclog.Logger as backend.
func NewHcLogLoggerAdapter(z hclog.Logger, hookKeys []string) Logger {
	return &HcLogLoggerAdapter{backend: z, hooks: hookKeys}
}

func (l *HcLogLoggerAdapter) parseArgs(args []any) (msg string, ret []any) {
	start := 0
	if len(args) > 0 {
		if str, ok := args[0].(string); ok {
			msg = str
			start++ // remove first argument
		}
	}
	return msg, args[start:]
}

func (l *HcLogLoggerAdapter) Error(args ...any) {
	msg, fields := l.parseArgs(args)
	l.Errorf(msg, fields...)
}

func (l *HcLogLoggerAdapter) Errorf(msg string, args ...any) {
	backend := l.fireHooks()
	backend.Error(fmt.Sprintf(msg, args...))
}

func (l *HcLogLoggerAdapter) Info(args ...any) {
	msg, fields := l.parseArgs(args)
	l.Infof(msg, fields...)
}

func (l *HcLogLoggerAdapter) Infof(msg string, args ...any) {
	backend := l.fireHooks()
	backend.Info(fmt.Sprintf(msg, args...))
}

func (l *HcLogLoggerAdapter) Debug(args ...any) {
	msg, fields := l.parseArgs(args)
	l.Debugf(msg, fields...)
}

func (l *HcLogLoggerAdapter) Debugf(msg string, args ...any) {
	backend := l.fireHooks()
	backend.Debug(fmt.Sprintf(msg, args...))
}

func (l *HcLogLoggerAdapter) WithFields(fields map[string]any) Logger {
	args := make([]any, 0, len(fields)*2)
	for key, val := range fields {
		args = append(args, key)
		args = append(args, val)
	}
	return &HcLogLoggerAdapter{
		ctx:     l.ctx,
		backend: l.backend.With(args...),
		hooks:   l.hooks,
	}
}

func (l *HcLogLoggerAdapter) WithField(key string, val any) Logger {
	return &HcLogLoggerAdapter{
		ctx:     l.ctx,
		backend: l.backend.With(key, val),
		hooks:   l.hooks,
	}
}

func (l *HcLogLoggerAdapter) WithContext(ctx context.Context) Logger {
	return &HcLogLoggerAdapter{
		ctx:     ctx,
		backend: l.backend,
		hooks:   l.hooks,
	}
}

// fireHooks emulates logrus' hook feature
func (l *HcLogLoggerAdapter) fireHooks() hclog.Logger {
	if l.ctx == nil {
		return l.backend
	}
	var args []any
	for _, key := range l.hooks {
		val := l.ctx.Value(key)
		if val == nil {
			continue
		}
		args = append(args, key)
		args = append(args, val)
	}
	return l.backend.With(args...)
}

func (l *HcLogLoggerAdapter) Writer() io.Writer {
	return l.backend.StandardWriter(&hclog.StandardLoggerOptions{})
}
