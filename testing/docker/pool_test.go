package docker

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/formancehq/go-libs/v2/logging"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/stretchr/testify/require"
)

type MockT struct {
	t            *testing.T
	cleanupFuncs []func()
	failCalled   bool
}

func (m *MockT) Helper() {
	m.t.Helper()
}

func (m *MockT) Cleanup(f func()) {
	m.cleanupFuncs = append(m.cleanupFuncs, f)
}

func (m *MockT) Errorf(format string, args ...interface{}) {
	m.t.Errorf(format, args...)
}

func (m *MockT) FailNow() {
	m.failCalled = true
}

func NewMockT(t *testing.T) *MockT {
	return &MockT{
		t:            t,
		cleanupFuncs: []func(){},
	}
}

func TestNewPool(t *testing.T) {
	mockT := NewMockT(t)
	logger := logging.Testing()
	
	pool := NewPool(mockT, logger)
	
	require.NotNil(t, pool, "Pool should not be nil")
	require.Equal(t, mockT, pool.t, "Pool should have the correct T")
	require.NotNil(t, pool.pool, "Docker pool should not be nil")
	require.Equal(t, logger, pool.logger, "Pool should have the correct logger")
}

func TestPool_T(t *testing.T) {
	mockT := NewMockT(t)
	logger := logging.Testing()
	
	pool := NewPool(mockT, logger)
	
	require.Equal(t, mockT, pool.T(), "T() should return the correct T")
}

func TestPool_Run(t *testing.T) {
	if os.Getenv("CI") == "true" {
		t.Skip("Skipping test in CI environment")
	}
	
	mockT := NewMockT(t)
	logger := logging.Testing()
	
	pool := NewPool(mockT, logger)
	
	cfg := Configuration{
		RunOptions: &dockertest.RunOptions{
			Repository: "alpine",
			Tag:        "latest",
			Cmd:        []string{"echo", "hello"},
		},
		HostConfigOptions: []func(config *docker.HostConfig){
			func(config *docker.HostConfig) {
				config.AutoRemove = true
			},
		},
		Timeout:            5 * time.Second,
		RetryCheckInterval: 100 * time.Millisecond,
	}
	
	resource := pool.Run(cfg)
	
	require.NotNil(t, resource, "Resource should not be nil")
	require.NotEmpty(t, resource.Container.ID, "Container ID should not be empty")
	
	checkCalled := false
	cfg.CheckFn = func(ctx context.Context, resource *dockertest.Resource) error {
		checkCalled = true
		return nil
	}
	
	resource = pool.Run(cfg)
	
	require.NotNil(t, resource, "Resource should not be nil")
	require.True(t, checkCalled, "Check function should be called")
	
	require.NotEmpty(t, mockT.cleanupFuncs, "Cleanup functions should be registered")
}

func TestPool_streamContainerLogs(t *testing.T) {
	
	if os.Getenv("CI") == "true" {
		t.Skip("Skipping test in CI environment")
	}
	
	mockT := NewMockT(t)
	logger := logging.Testing()
	
	pool := NewPool(mockT, logger)
	
	cfg := Configuration{
		RunOptions: &dockertest.RunOptions{
			Repository: "alpine",
			Tag:        "latest",
			Cmd:        []string{"echo", "hello"},
		},
	}
	
	resource := pool.Run(cfg)
	
	pool.streamContainerLogs(resource.Container.ID)
}
