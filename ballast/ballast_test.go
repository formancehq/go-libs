package ballast_test

import (
	"runtime"
	"testing"

	"github.com/formancehq/go-libs/v2/ballast"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
)

func TestAllocate(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name   string
		size   uint
		expect bool
	}{
		{
			name:   "zero size",
			size:   0,
			expect: false,
		},
		{
			name:   "positive size",
			size:   1024 * 1024, // 1MB
			expect: true,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var memStatsBefore runtime.MemStats
			runtime.ReadMemStats(&memStatsBefore)

			ballast.Allocate(tc.size)
			
			var memStatsAfter runtime.MemStats
			runtime.ReadMemStats(&memStatsAfter)

			if tc.expect {
				// Verify memory usage increased
				require.Greater(t, memStatsAfter.Alloc, memStatsBefore.Alloc)
			}
			
			// Clean up
			ballast.ReleaseForGC()
		})
	}
}

func TestReleaseForGC(t *testing.T) {
	t.Parallel()
	
	// Allocate some memory
	ballast.Allocate(1024 * 1024) // 1MB
	
	// Release it
	ballast.ReleaseForGC()
	
	// No way to verify directly, but at least ensure it doesn't panic
	require.True(t, true)
}

func TestBallastModule(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name string
		size uint
	}{
		{
			name: "zero size",
			size: 0,
		},
		{
			name: "positive size",
			size: 1024 * 1024, // 1MB
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var options []fx.Option
			options = append(options, ballast.Module(tc.size))
			
			app := fx.New(options...)
			require.NoError(t, app.Err())
		})
	}
}
