package queue

import "time"

type ListenerOptions struct {
	Name             string
	WorkerCount      int
	CallbackDeadline time.Duration
}

// name may appear in logs making debugging easier
func WithName(n string) func(*ListenerOptions) {
	return func(o *ListenerOptions) {
		o.Name = n
	}
}

// how many workers to spawn - defaults to 1 if not set
func WithWorkerCount(n int) func(*ListenerOptions) {
	return func(o *ListenerOptions) {
		o.WorkerCount = n
	}
}

// maximum time to wait for a single callback to finish
// helps prevent leaking goroutines in case the listener shuts down
func WithCallbackDeadline(d time.Duration) func(*ListenerOptions) {
	return func(o *ListenerOptions) {
		o.CallbackDeadline = d
	}
}
