//go:build pyroscope

package pyroscope

import (
	"context"
	"errors"
	"fmt"
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
	scrub  func(string) string
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
	scrub := h.cfg.scrubber()
	h.scrub = scrub
	h.logger.WithContext(ctx).WithFields(map[string]any{
		"server_address": scrub(h.cfg.ServerAddress), "application_name": scrub(h.cfg.ApplicationName),
		"profile_types": h.cfg.ProfileTypes, "tags": scrub(fmt.Sprint(h.cfg.Tags)),
	}).Infof("Starting Pyroscope profiler")
	config := h.cfg.Config
	config.Logger = &sdkLogger{logger: h.logger, scrub: scrub}
	stop, err := h.start(config)
	if err != nil {
		// Do not retain an SDK error chain that may contain a credential-bearing URL.
		return errors.New("starting pyroscope profiler: " + scrub(err.Error()))
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
		h.logger.WithContext(ctx).WithField("error", h.scrub(err.Error())).Infof("Error stopping Pyroscope profiler")
	}
	h.stop = nil
	return nil
}

type sdkLogger struct {
	logger logging.Logger
	scrub  func(string) string
}

func (l *sdkLogger) Infof(format string, args ...any) {
	l.logger.Infof("%s", l.scrub(fmt.Sprintf(format, args...)))
}
func (l *sdkLogger) Errorf(format string, args ...any) {
	l.logger.Errorf("%s", l.scrub(fmt.Sprintf(format, args...)))
}
func (l *sdkLogger) Debugf(format string, args ...any) {
	if l.logger.Enabled(logging.DebugLevel) {
		l.logger.Debugf("%s", l.scrub(fmt.Sprintf(format, args...)))
	}
}
