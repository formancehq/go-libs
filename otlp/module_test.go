package otlp

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

func TestNewFxModule(t *testing.T) {
	cfg := Config{
		ServiceName:        "test-service",
		ResourceAttributes: []string{"key1=value1", "key2=value2"},
		serviceVersion:     "1.0.0",
	}
	
	module := NewFxModule(cfg)
	require.NotNil(t, module, "Le module ne devrait pas être nil")
}

func TestWithServiceVersion(t *testing.T) {
	cfg := Config{}
	opt := WithServiceVersion("1.0.0")
	opt(&cfg)
	
	require.Equal(t, "1.0.0", cfg.serviceVersion, "La version du service devrait être correctement définie")
}

func TestNewConfig(t *testing.T) {
	opts := []Option{
		WithServiceVersion("1.0.0"),
	}
	
	cfg := newConfig(opts)
	require.Equal(t, "1.0.0", cfg.serviceVersion, "La version du service devrait être correctement définie")
	require.Empty(t, cfg.ServiceName, "Le nom du service devrait être vide")
	require.Empty(t, cfg.ResourceAttributes, "Les attributs de ressource devraient être vides")
}

func TestFXModuleFromFlags(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String(OtelServiceNameFlag, "test-service", "")
	cmd.Flags().StringSlice(OtelResourceAttributesFlag, []string{"key=value"}, "")
	
	module := FXModuleFromFlags(cmd, WithServiceVersion("1.0.0"))
	require.NotNil(t, module, "Le module ne devrait pas être nil")
}

func TestFXModuleFromFlags_WithoutFlags(t *testing.T) {
	cmd := &cobra.Command{}
	
	module := FXModuleFromFlags(cmd, WithServiceVersion("1.0.0"))
	require.NotNil(t, module, "Le module ne devrait pas être nil")
}

func TestAddFlags(t *testing.T) {
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	AddFlags(flags)
	
	serviceName, err := flags.GetString(OtelServiceNameFlag)
	require.NoError(t, err)
	require.Empty(t, serviceName, "Le nom du service devrait être vide par défaut")
	
	resourceAttrs, err := flags.GetStringSlice(OtelResourceAttributesFlag)
	require.NoError(t, err)
	require.Empty(t, resourceAttrs, "Les attributs de ressource devraient être vides par défaut")
}

func TestLoadResource(t *testing.T) {
	option := LoadResource("test-service", []string{"key=value"}, "1.0.0")
	require.NotNil(t, option, "L'option ne devrait pas être nil")
}
