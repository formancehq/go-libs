package logging

import (
	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"go.uber.org/zap"
)

// NewLogr exposes z as a logr.Logger, the interface controller-runtime and klog
// log through. An operator that passes it to both ctrl.SetLogger and
// klog.SetLogger puts its own records, the controller-runtime machinery and
// client-go's leader election and API warnings on the encoder every other
// service uses.
//
// Note that logr's verbosity maps onto zap's negative levels: V(1) is Debug and
// V(2) is the custom trace level, which ZapEncoderConfig renders as "TRACE"
// rather than zap's "Level(-2)".
func NewLogr(z *zap.Logger) logr.Logger {
	return zapr.NewLogger(z)
}
