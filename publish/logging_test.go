package publish_test

import (
	"errors"
	"testing"

	"github.com/ThreeDotsLabs/watermill"
	"go.uber.org/mock/gomock"

	"github.com/formancehq/go-libs/v4/logging"
	"github.com/formancehq/go-libs/v4/publish"
)

func TestWatermillLoggerAdapter_Error(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockLogger := logging.NewMockLogger(ctrl)
	fields := watermill.LogFields{"key": "value"}
	err := errors.New("test error")

	mockLogger.EXPECT().WithFields(gomock.Any()).Times(2).Return(mockLogger)
	mockLogger.EXPECT().Error("error message").Return()

	logger := publish.NewWatermillLoggerAdapter(mockLogger, false)
	logger.Error("error message", err, fields)
}

func TestWatermillLoggerAdapter_Info(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockLogger := logging.NewMockLogger(ctrl)
	fields := watermill.LogFields{"key": "value"}

	mockLogger.EXPECT().WithFields(gomock.Any()).Return(mockLogger)
	mockLogger.EXPECT().Info("info message").Return()

	logger := publish.NewWatermillLoggerAdapter(mockLogger, false)
	logger.Info("info message", fields)
}

func TestWatermillLoggerAdapter_Debug(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockLogger := logging.NewMockLogger(ctrl)
	fields := watermill.LogFields{"key": "value"}

	mockLogger.EXPECT().WithFields(gomock.Any()).Return(mockLogger)
	mockLogger.EXPECT().Debug("debug message").Return()

	logger := publish.NewWatermillLoggerAdapter(mockLogger, false)
	logger.Debug("debug message", fields)
}

func TestWatermillLoggerAdapter_Trace(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockLogger := logging.NewMockLogger(ctrl)
	fields := watermill.LogFields{"key": "value"}

	mockLogger.EXPECT().WithFields(gomock.Any()).Return(mockLogger)
	mockLogger.EXPECT().Debug("trace message").Return()

	logger := publish.NewWatermillLoggerAdapter(mockLogger, true)
	logger.Trace("trace message", fields)
}

func TestWatermillLoggerAdapter_Trace_Disabled(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockLogger := logging.NewMockLogger(ctrl)
	fields := watermill.LogFields{"key": "value"}

	logger := publish.NewWatermillLoggerAdapter(mockLogger, false)
	logger.Trace("trace message", fields)
}
