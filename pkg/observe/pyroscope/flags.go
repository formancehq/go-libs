//go:build pyroscope

package pyroscope

import (
	"maps"
	"slices"
	"strings"

	sdk "github.com/grafana/pyroscope-go"
	"github.com/spf13/pflag"

	"github.com/formancehq/go-libs/v5/pkg/observe/pyroscopetraces"
)

const (
	EnabledFlag              = "pyroscope-enabled"
	ServerAddressFlag        = "pyroscope-server-address"
	ApplicationNameFlag      = "pyroscope-application-name"
	AuthTokenFlag            = "pyroscope-auth-token"
	TenantIDFlag             = "pyroscope-tenant-id"
	BasicAuthUserFlag        = "pyroscope-basic-auth-user"
	BasicAuthPasswordFlag    = "pyroscope-basic-auth-password"
	UploadRateFlag           = "pyroscope-upload-rate"
	TagsFlag                 = "pyroscope-tags"
	ProfileTypesFlag         = "pyroscope-profile-types"
	MutexProfileFractionFlag = "pyroscope-mutex-profile-fraction"
	BlockProfileRateFlag     = "pyroscope-block-profile-rate"
	DisableGCRunsFlag        = "pyroscope-disable-gc-runs"
)

func profileTypes() map[string]sdk.ProfileType {
	return map[string]sdk.ProfileType{
		"cpu": sdk.ProfileCPU, "alloc_objects": sdk.ProfileAllocObjects, "alloc_space": sdk.ProfileAllocSpace,
		"inuse_objects": sdk.ProfileInuseObjects, "inuse_space": sdk.ProfileInuseSpace, "goroutines": sdk.ProfileGoroutines,
		"mutex_count": sdk.ProfileMutexCount, "mutex_duration": sdk.ProfileMutexDuration,
		"block_count": sdk.ProfileBlockCount, "block_duration": sdk.ProfileBlockDuration,
	}
}

// AddFlags registers native profiling settings; pprof HTTP endpoints remain a
// separate capability. Span-correlation flags reuse pyroscopetraces vocabulary.
func AddFlags(flags *pflag.FlagSet) {
	cfg := DefaultConfig()
	flags.Bool(EnabledFlag, false, "Enable Pyroscope continuous profiling")
	for _, field := range []struct{ name, value, help string }{
		{ServerAddressFlag, cfg.ServerAddress, "Pyroscope server address"},
		{ApplicationNameFlag, "", "Application name for Pyroscope"},
		{AuthTokenFlag, "", "Authentication token for Pyroscope"},
		{TenantIDFlag, "", "Tenant ID for multi-tenant Pyroscope"},
		{BasicAuthUserFlag, "", "Basic auth username for Pyroscope"},
		{BasicAuthPasswordFlag, "", "Basic auth password for Pyroscope"},
	} {
		flags.String(field.name, field.value, field.help)
	}
	flags.Duration(UploadRateFlag, cfg.UploadRate, "Profile upload rate")
	flags.StringSlice(TagsFlag, []string{}, "Additional tags (key=value)")
	flags.StringSlice(ProfileTypesFlag, slices.Sorted(maps.Keys(profileTypes())), "Profile types to enable")
	flags.Int(MutexProfileFractionFlag, cfg.MutexProfileFraction, "Mutex profile fraction")
	flags.Int(BlockProfileRateFlag, cfg.BlockProfileRate, "Block profile rate")
	flags.Bool(DisableGCRunsFlag, false, "Disable automatic GC runs")
	pyroscopetraces.AddFlags(flags)
}

// ConfigFromFlags preserves lenient diagnostic configuration: malformed tag
// pairs and unknown profile names are skipped. Required flag types come from
// AddFlags. Collection values are read without a lossy CSV getter round trip.
func ConfigFromFlags(flags *pflag.FlagSet) Config {
	cfg := DefaultConfig()
	cfg.Enabled, _ = flags.GetBool(EnabledFlag)
	cfg.ServerAddress, _ = flags.GetString(ServerAddressFlag)
	cfg.ApplicationName, _ = flags.GetString(ApplicationNameFlag)
	cfg.AuthToken, _ = flags.GetString(AuthTokenFlag)
	cfg.TenantID, _ = flags.GetString(TenantIDFlag)
	cfg.BasicAuthUser, _ = flags.GetString(BasicAuthUserFlag)
	cfg.BasicAuthPassword, _ = flags.GetString(BasicAuthPasswordFlag)
	cfg.UploadRate, _ = flags.GetDuration(UploadRateFlag)
	cfg.MutexProfileFraction, _ = flags.GetInt(MutexProfileFractionFlag)
	cfg.BlockProfileRate, _ = flags.GetInt(BlockProfileRateFlag)
	cfg.DisableGCRuns, _ = flags.GetBool(DisableGCRunsFlag)
	cfg.TagSpans, _ = flags.GetBool(pyroscopetraces.OtelTracesProviderPyroscopeEnabledFlag)
	cfg.TagAllSpans, _ = flags.GetBool(pyroscopetraces.OtelTracesProviderPyroscopeAllSpansFlag)
	for _, pair := range sliceValue(flags, TagsFlag) {
		if key, value, ok := strings.Cut(strings.TrimSpace(pair), "="); ok {
			cfg.Tags[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	names := sliceValue(flags, ProfileTypesFlag)
	if len(names) > 0 {
		cfg.ProfileTypes = nil
		available := profileTypes()
		for _, name := range names {
			if value, ok := available[strings.ToLower(strings.TrimSpace(name))]; ok {
				cfg.ProfileTypes = append(cfg.ProfileTypes, value)
			}
		}
	}
	return cfg
}

func sliceValue(flags *pflag.FlagSet, name string) []string {
	setting := flags.Lookup(name)
	if setting == nil {
		return nil
	}
	if value, ok := setting.Value.(pflag.SliceValue); ok {
		return value.GetSlice()
	}
	return nil
}
