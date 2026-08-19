package observefx_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/formancehq/go-libs/v5/pkg/fx/observefx"
	"github.com/formancehq/go-libs/v5/pkg/observe"
)

func TestResourceModuleProvidesConfiguredResource(t *testing.T) {
	var res *resource.Resource

	app := fxtest.New(t,
		observefx.ResourceModule(observe.Config{
			ServiceName:        "resource-test",
			ResourceAttributes: []string{"stack.id=stack-123"},
		}),
		fx.Populate(&res),
		fx.NopLogger,
	)
	app.RequireStart()
	defer app.RequireStop()

	serviceName, ok := res.Set().Value(attribute.Key("service.name"))
	require.True(t, ok, "resource must carry service.name from Config.ServiceName")
	require.Equal(t, "resource-test", serviceName.AsString())

	stackID, ok := res.Set().Value(attribute.Key("stack.id"))
	require.True(t, ok, "resource must carry attributes from Config.ResourceAttributes")
	require.Equal(t, "stack-123", stackID.AsString())
}
