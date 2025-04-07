package ballast

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAllocate(t *testing.T) {
	var m1, m2 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m1)

	size := uint(10 * 1024 * 1024)
	Allocate(size)

	runtime.ReadMemStats(&m2)

	require.NotNil(t, ballast, "Le ballast devrait être alloué")

	require.Greater(t, m2.Alloc, m1.Alloc, "L'allocation de mémoire devrait avoir augmenté")
}

func TestReleaseForGC(t *testing.T) {
	Allocate(1024)
	require.NotNil(t, ballast, "Le ballast devrait être alloué avant libération")

	ReleaseForGC()

	require.Nil(t, ballast, "Le ballast devrait être libéré")
}

func TestAllocateMultipleTimes(t *testing.T) {
	Allocate(1024)
	firstBallast := ballast
	require.NotNil(t, firstBallast, "Le premier ballast devrait être alloué")

	Allocate(2048)
	secondBallast := ballast
	require.NotNil(t, secondBallast, "Le second ballast devrait être alloué")

	ReleaseForGC()
	require.Nil(t, ballast, "Le ballast devrait être libéré")
}

func TestAllocateZeroSize(t *testing.T) {
	Allocate(0)

	require.NotNil(t, ballast, "Le ballast devrait être alloué même avec une taille zéro")

	ReleaseForGC()
	require.Nil(t, ballast, "Le ballast devrait être libéré")
}
