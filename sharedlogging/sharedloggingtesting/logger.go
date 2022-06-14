package sharedloggingtesting

import (
	"github.com/numary/go-libs/sharedlogging"
	"github.com/numary/go-libs/sharedlogging/sharedlogginglogrus"
	"github.com/sirupsen/logrus"
	"testing"
)

func Logger() sharedlogging.Logger {
	l := logrus.New()
	if testing.Verbose() {
		l.SetLevel(logrus.DebugLevel)
	}
	return sharedlogginglogrus.New(l)
}
