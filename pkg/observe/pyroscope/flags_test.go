//go:build pyroscope

package pyroscope

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	sdk "github.com/grafana/pyroscope-go"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

func TestFlagsDefaultsAndSampling(t *testing.T) {
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	AddFlags(flags)
	cfg := ConfigFromFlags(flags)
	require.False(t, cfg.Enabled)
	require.Equal(t, "http://localhost:4040", cfg.ServerAddress)
	require.Equal(t, 15*time.Second, cfg.UploadRate)
	require.Equal(t, 5, cfg.MutexProfileFraction)
	require.Equal(t, 5, cfg.BlockProfileRate)
	require.Len(t, cfg.ProfileTypes, 10)
	require.Equal(t, "[alloc_objects,alloc_space,block_count,block_duration,cpu,goroutines,inuse_objects,inuse_space,mutex_count,mutex_duration]", flags.Lookup(ProfileTypesFlag).DefValue)
	require.Equal(t, DefaultConfig().ProfileTypes, []sdk.ProfileType{sdk.ProfileCPU, sdk.ProfileAllocObjects, sdk.ProfileAllocSpace, sdk.ProfileInuseObjects, sdk.ProfileInuseSpace, sdk.ProfileGoroutines, sdk.ProfileMutexCount, sdk.ProfileMutexDuration, sdk.ProfileBlockCount, sdk.ProfileBlockDuration})
}

func TestFlagValuesLenientCollectionsAndSecrets(t *testing.T) {
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	AddFlags(flags)
	require.NoError(t, flags.Parse([]string{
		"--pyroscope-enabled", "--pyroscope-server-address=https://profiles.example.test", "--pyroscope-application-name=service",
		"--pyroscope-auth-token=synthetic-token", "--pyroscope-basic-auth-user=synthetic-user", "--pyroscope-basic-auth-password=synthetic-password",
		"--pyroscope-tenant-id=tenant", "--pyroscope-upload-rate=45s", "--pyroscope-mutex-profile-fraction=9", "--pyroscope-block-profile-rate=11",
		"--pyroscope-disable-gc-runs", "--pyroscope-profile-types=cpu,unknown, INUSE_SPACE ", "--pyroscope-tags=bare, region = eu,key=a=b,=empty,  =spaces",
		"--otel-traces-provider-pyroscope-enabled", "--otel-traces-provider-pyroscope-all-spans",
	}))
	cfg := ConfigFromFlags(flags)
	require.True(t, cfg.Enabled)
	require.Equal(t, "service", cfg.ApplicationName)
	require.Equal(t, "synthetic-token", cfg.AuthToken)
	require.Equal(t, "synthetic-user", cfg.BasicAuthUser)
	require.Equal(t, "synthetic-password", cfg.BasicAuthPassword)
	require.Equal(t, "tenant", cfg.TenantID)
	require.Equal(t, 45*time.Second, cfg.UploadRate)
	require.Equal(t, 9, cfg.MutexProfileFraction)
	require.Equal(t, 11, cfg.BlockProfileRate)
	require.True(t, cfg.DisableGCRuns)
	require.True(t, cfg.TagSpans)
	require.True(t, cfg.TagAllSpans)
	require.Equal(t, map[string]string{"region": "eu", "key": "a=b"}, cfg.Tags)
	require.Equal(t, []sdk.ProfileType{sdk.ProfileCPU, sdk.ProfileInuseSpace}, cfg.ProfileTypes)
	for _, formatted := range []string{fmt.Sprint(cfg), fmt.Sprintf("%+v", cfg), fmt.Sprintf("%#v", cfg)} {
		for _, secret := range []string{"synthetic-token", "synthetic-user", "synthetic-password"} {
			require.NotContains(t, formatted, secret)
		}
	}
	encoded, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "synthetic-password")
}

func TestCollectionsReadStoredValues(t *testing.T) {
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	AddFlags(flags)
	require.NoError(t, flags.Lookup(TagsFlag).Value.(pflag.SliceValue).Replace([]string{" key = line\r\nnext ", "a=b,c", ""}))
	cfg := ConfigFromFlags(flags)
	require.Equal(t, "line\r\nnext", cfg.Tags["key"])
	require.Equal(t, "b,c", cfg.Tags["a"])
	require.NoError(t, flags.Lookup(ProfileTypesFlag).Value.(pflag.SliceValue).Replace([]string{""}))
	require.Empty(t, ConfigFromFlags(flags).ProfileTypes)
	require.NoError(t, flags.Lookup(ProfileTypesFlag).Value.(pflag.SliceValue).Replace([]string{}))
	require.Equal(t, DefaultConfig().ProfileTypes, ConfigFromFlags(flags).ProfileTypes)
}
