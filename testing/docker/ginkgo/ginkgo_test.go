package ginkgo

import (
	"testing"

	"github.com/formancehq/go-libs/v2/logging"
	"github.com/formancehq/go-libs/v2/testing/docker"
	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"
)

func TestActualDockerPool(t *testing.T) {
	result := ActualDockerPool()
	require.Same(t, pool, result, "ActualDockerPool should return the pool variable")
}

func TestWithNewDockerPool(t *testing.T) {
	originalContext := ginkgo.Context
	originalBeforeEach := ginkgo.BeforeEach
	originalGinkgoT := ginkgo.GinkgoT
	
	defer func() {
		ginkgo.Context = originalContext
		ginkgo.BeforeEach = originalBeforeEach
		ginkgo.GinkgoT = originalGinkgoT
	}()
	
	contextCalled := false
	beforeEachCalled := false
	fnCalled := false
	
	ginkgo.Context = func(text string, body func()) bool {
		require.Equal(t, "With docker pool", text, "Context text should match")
		contextCalled = true
		body()
		return true
	}
	
	ginkgo.BeforeEach = func(body interface{}) {
		beforeEachCalled = true
		bodyFunc, ok := body.(func())
		require.True(t, ok, "BeforeEach body should be a function")
		
	}
	
	ginkgo.GinkgoT = func() ginkgo.GinkgoTInterface {
		return &mockGinkgoT{t: t}
	}
	
	logger := logging.Testing()
	result := WithNewDockerPool(logger, func() {
		fnCalled = true
	})
	
	require.True(t, result, "WithNewDockerPool should return true")
	require.True(t, contextCalled, "Context should be called")
	require.True(t, beforeEachCalled, "BeforeEach should be called")
	require.True(t, fnCalled, "Function should be called")
}

type mockGinkgoT struct {
	t *testing.T
}

func (m *mockGinkgoT) Helper() {}

func (m *mockGinkgoT) Cleanup(f func()) {
	m.t.Cleanup(f)
}

func (m *mockGinkgoT) Errorf(format string, args ...interface{}) {
	m.t.Errorf(format, args...)
}

func (m *mockGinkgoT) FailNow() {
	m.t.FailNow()
}
