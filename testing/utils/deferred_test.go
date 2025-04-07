package utils

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewDeferred(t *testing.T) {
	d := NewDeferred[string]()
	require.NotNil(t, d, "Deferred should not be nil")
	require.NotNil(t, d.set, "Set channel should not be nil")
	require.Nil(t, d.value, "Value should be nil initially")
}

func TestDeferred_SetValue(t *testing.T) {
	d := NewDeferred[string]()
	
	d.SetValue("test")
	
	require.NotNil(t, d.value, "Value should not be nil after setting")
	require.Equal(t, "test", *d.value, "Value should match what was set")
	
	select {
	case <-d.set:
	default:
		t.Fatal("Set channel should be closed after setting value")
	}
}

func TestDeferred_GetValue(t *testing.T) {
	d := NewDeferred[string]()
	d.SetValue("test")
	
	value := d.GetValue()
	
	require.Equal(t, "test", value, "GetValue should return the set value")
}

func TestDeferred_Reset(t *testing.T) {
	d := NewDeferred[string]()
	d.SetValue("test")
	
	d.Reset()
	
	require.Nil(t, d.value, "Value should be nil after reset")
	
	select {
	case <-d.set:
		t.Fatal("Set channel should not be closed after reset")
	default:
	}
}

func TestDeferred_Done(t *testing.T) {
	d := NewDeferred[string]()
	
	require.Equal(t, d.set, d.Done(), "Done should return the set channel")
}

func TestDeferred_LoadAsync(t *testing.T) {
	d := NewDeferred[string]()
	
	d.LoadAsync(func() string {
		time.Sleep(50 * time.Millisecond) // Simulate work
		return "async test"
	})
	
	select {
	case <-d.set:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Timed out waiting for async value to be set")
	}
	
	require.NotNil(t, d.value, "Value should not be nil after async setting")
	require.Equal(t, "async test", *d.value, "Value should match what was set asynchronously")
}

func TestWait(t *testing.T) {
	d1 := NewDeferred[string]()
	d2 := NewDeferred[int]()
	
	go func() {
		time.Sleep(50 * time.Millisecond)
		d1.SetValue("test1")
	}()
	
	go func() {
		time.Sleep(75 * time.Millisecond)
		d2.SetValue(42)
	}()
	
	done := make(chan struct{})
	
	go func() {
		Wait(d1, d2)
		close(done)
	}()
	
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Timed out waiting for Wait to return")
	}
	
	require.Equal(t, "test1", d1.GetValue(), "First deferred value should be set")
	require.Equal(t, 42, d2.GetValue(), "Second deferred value should be set")
}
