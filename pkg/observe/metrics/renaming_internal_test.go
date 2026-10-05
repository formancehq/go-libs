package metrics

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric"
)

// TestRenamingMeterOverridesEveryConstructor fails when an OpenTelemetry
// upgrade adds an instrument constructor to metric.Meter that renamingMeter
// does not override. The embedded metric.Meter would otherwise forward it
// silently, and instruments created with it would escape the naming policy.
func TestRenamingMeterOverridesEveryConstructor(t *testing.T) {
	t.Parallel()

	file, err := parser.ParseFile(token.NewFileSet(), "renaming.go", nil, 0)
	require.NoError(t, err)

	overridden := map[string]bool{}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 {
			continue
		}
		star, ok := fn.Recv.List[0].Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		if ident, ok := star.X.(*ast.Ident); ok && ident.Name == "renamingMeter" {
			overridden[fn.Name.Name] = true
		}
	}

	// Every exported method except RegisterCallback creates an instrument
	// from a name, so it must be overridden; the unexported embedded.Meter
	// marker is forwarded on purpose.
	constructors := 0
	for method := range reflect.TypeFor[metric.Meter]().Methods() {
		if !method.IsExported() || method.Name == "RegisterCallback" {
			continue
		}
		constructors++
		require.True(t, overridden[method.Name], "renamingMeter does not override metric.Meter.%s", method.Name)
	}
	require.NotZero(t, constructors)
}
