package utils

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTestingTForMain_Helper(t *testing.T) {
	tm := &TestingTForMain{}
	tm.Helper()
}

func TestTestingTForMain_Errorf(t *testing.T) {
	tm := &TestingTForMain{}
	
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	
	tm.Errorf("test %s", "message")
	
	w.Close()
	os.Stderr = oldStderr
	
	var buf [100]byte
	n, _ := r.Read(buf[:])
	output := string(buf[:n])
	
	require.Equal(t, "test message", output, "Errorf should write formatted message to stderr")
}

func TestTestingTForMain_TempDir(t *testing.T) {
	tm := &TestingTForMain{}
	dir := tm.TempDir()
	require.Equal(t, os.TempDir(), dir, "TempDir should return os.TempDir()")
}

func TestTestingTForMain_Cleanup(t *testing.T) {
	tm := &TestingTForMain{}
	
	called := false
	tm.Cleanup(func() {
		called = true
	})
	
	require.Len(t, tm.cleanup, 1, "Cleanup should add function to cleanup slice")
	
	tm.callCleanup()
	
	require.True(t, called, "Cleanup function should be called")
}

func TestTestingTForMain_callCleanup(t *testing.T) {
	tm := &TestingTForMain{}
	
	order := []int{}
	
	tm.Cleanup(func() {
		order = append(order, 1)
	})
	
	tm.Cleanup(func() {
		order = append(order, 2)
	})
	
	tm.Cleanup(func() {
		order = append(order, 3)
	})
	
	tm.callCleanup()
	
	require.Equal(t, []int{3, 2, 1}, order, "Cleanup functions should be called in reverse order")
}

func TestTestingTForMain_close(t *testing.T) {
	tm := &TestingTForMain{}
	
	called := false
	tm.Cleanup(func() {
		called = true
	})
	
	tm.close()
	
	require.True(t, called, "close should call cleanup functions")
}

func mockWithTestMain(fn func(main *TestingTForMain) int) int {
	t := &TestingTForMain{}
	code := fn(t)
	t.close()
	return code
}

func TestWithTestMain(t *testing.T) {
	originalWithTestMain := WithTestMain
	
	WithTestMain = func(fn func(main *TestingTForMain) int) {
		code := mockWithTestMain(fn)
		require.Equal(t, 42, code, "WithTestMain should return the code from the function")
	}
	
	defer func() {
		WithTestMain = originalWithTestMain
	}()
	
	WithTestMain(func(main *TestingTForMain) int {
		require.NotNil(t, main, "TestingTForMain should not be nil")
		return 42
	})
}
