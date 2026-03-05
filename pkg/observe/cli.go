package observe

import (
	"fmt"
	"strings"

	flag "github.com/spf13/pflag"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
)

const (
	OtelResourceAttributesFlag = "otel-resource-attributes"
	OtelServiceNameFlag        = "otel-service-name"
)

func AddFlags(flags *flag.FlagSet) {
	if flags.Lookup(OtelServiceNameFlag) == nil {
		flags.String(OtelServiceNameFlag, "", "OpenTelemetry service name")
		flags.StringSlice(OtelResourceAttributesFlag, []string{}, "Additional OTLP resource attributes")
	}
}

func BuildResource(serviceName string, resourceAttributes []string, version string) (*resource.Resource, error) {
	defaultResource := resource.Default()
	attributes := make([]attribute.KeyValue, 0)
	if serviceName != "" {
		attributes = append(attributes, attribute.String("service.name", serviceName))
	}

	if version != "" {
		attributes = append(attributes, attribute.String("service.version", version))
	}
	for _, ra := range resourceAttributes {
		parts := strings.SplitN(ra, "=", 2)
		if len(parts) < 2 {
			return nil, fmt.Errorf("malformed otlp attribute: %s", ra)
		}
		attributes = append(attributes, attribute.String(parts[0], parts[1]))
	}
	return resource.Merge(defaultResource, resource.NewSchemaless(attributes...))
}
