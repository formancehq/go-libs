//go:build pyroscope

// Package pyroscope configures native continuous profiling. It is optional at
// build time; compile with the pyroscope tag to include the native SDK.
package pyroscope

import (
	"runtime"
	"time"

	sdk "github.com/grafana/pyroscope-go"
)

// Config embeds the native SDK configuration, without redefining its fields.
// Enabled gates native profiling and trace correlation. Sampling rates are
// process-wide; non-positive values leave the corresponding runtime rate alone.
type Config struct {
	sdk.Config
	Enabled              bool
	MutexProfileFraction int
	BlockProfileRate     int
	TagSpans             bool
	TagAllSpans          bool
}

func DefaultConfig() Config {
	return Config{
		Config: sdk.Config{ServerAddress: "http://localhost:4040", Tags: map[string]string{}, UploadRate: 15 * time.Second,
			ProfileTypes: []sdk.ProfileType{sdk.ProfileCPU, sdk.ProfileAllocObjects, sdk.ProfileAllocSpace, sdk.ProfileInuseObjects, sdk.ProfileInuseSpace, sdk.ProfileGoroutines, sdk.ProfileMutexCount, sdk.ProfileMutexDuration, sdk.ProfileBlockCount, sdk.ProfileBlockDuration}},
		MutexProfileFraction: 5, BlockProfileRate: 5,
	}
}

// SetupRuntimeProfiling applies positive sampling rates, without restoring them
// on shutdown. The Fx module calls it during graph construction, matching the
// configuration-before-start contract. Disabled configurations have no effect.
func (c Config) SetupRuntimeProfiling() {
	if !c.Enabled {
		return
	}
	if c.MutexProfileFraction > 0 {
		runtime.SetMutexProfileFraction(c.MutexProfileFraction)
	}
	if c.BlockProfileRate > 0 {
		runtime.SetBlockProfileRate(c.BlockProfileRate)
	}
}
