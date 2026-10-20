//go:build pyroscope

package pyroscope

import (
	"context"
	"errors"
	"maps"
	"slices"
	"sync"

	sdk "github.com/grafana/pyroscope-go"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
)

// Hook owns the native SDK start/stop lifecycle without importing Fx. NewHook
// is pure: callers explicitly apply Config.SetupRuntimeProfiling when desired.
// OnStop joins SDK shutdown and may exceed its context deadline: the native SDK
// Stop API has no context. Stop failures are diagnostic and do not fail shutdown.
type Hook struct {
	mu     sync.Mutex
	cfg    Config
	logger logging.Logger
	start  func(sdk.Config) (func() error, error)
	stop   func() error
}

func NewHook(cfg Config, logger logging.Logger) *Hook {
	if logger == nil {
		logger = logging.NopZap()
	}
	cfg.Tags = maps.Clone(cfg.Tags)
	cfg.ProfileTypes = slices.Clone(cfg.ProfileTypes)
	cfg.HTTPHeaders = maps.Clone(cfg.HTTPHeaders)
	return &Hook{cfg: cfg, logger: logger, start: func(config sdk.Config) (func() error, error) {
		profiler, err := sdk.Start(config)
		if err != nil {
			return nil, err
		}
		return profiler.Stop, nil
	}}
}

func (h *Hook) OnStart(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.cfg.Enabled || h.stop != nil {
		return nil
	}
	if h.cfg.UploadRate < 0 {
		return errors.New("pyroscope: upload rate must not be negative")
	}
	h.logger.WithContext(ctx).Infof("Starting Pyroscope profiler")
	config := h.cfg.Config
	config.Logger = &sdkLogger{logger: h.logger}
	stop, err := h.start(config)
	if err != nil {
		// Do not retain an SDK error chain that may contain a credential-bearing URL.
		return errors.New("starting pyroscope profiler failed")
	}
	h.stop = stop
	return nil
}

func (h *Hook) OnStop(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stop == nil {
		return nil
	}
	h.logger.WithContext(ctx).Infof("Stopping Pyroscope profiler")
	if err := h.stop(); err != nil {
		h.logger.WithContext(ctx).Infof("Error stopping Pyroscope profiler")
	}
	h.stop = nil
	return nil
}

type sdkLogger struct {
	logger logging.Logger
}

func (l *sdkLogger) Infof(string, ...any) {
	l.logger.Infof("Pyroscope SDK event")
}
func (l *sdkLogger) Errorf(string, ...any) {
	l.logger.Errorf("Pyroscope SDK event")
}
func (l *sdkLogger) Debugf(string, ...any) {
	if l.logger.Enabled(logging.DebugLevel) {
		l.logger.Debugf("Pyroscope SDK event")
	}
}
