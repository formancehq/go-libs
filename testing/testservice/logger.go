package testservice

type Logger interface {
	Logf(fmt string, args ...any)
}
type LoggerFunc func(fmt string, args ...any)

func (f LoggerFunc) Logf(fmt string, args ...any) {
	f(fmt, args...)
}

var NoOpLogger = LoggerFunc(func(fmt string, args ...any) {
	// no-op
})
