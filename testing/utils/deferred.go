package utils

import (
	. "github.com/onsi/ginkgo/v2"
)

type Deferred[V any] struct {
	value *V
	set   chan struct{}
}

func (d *Deferred[V]) GetValue() V {
	return *d.value
}

func (d *Deferred[V]) LoadAsync(fn func() V) {
	go func() {
		d.SetValue(fn())
	}()
}

func (d *Deferred[V]) SetValue(v V) {
	d.value = &v
	close(d.set)
}

func (d *Deferred[V]) Reset() {
	if d.set != nil {
		select {
		case <-d.set:
			// already closed
		default:
			close(d.set)
		}
	}
	d.set = make(chan struct{})
	d.value = nil
}

func (d *Deferred[V]) Done() chan struct{} {
	return d.set
}

func (d *Deferred[V]) Wait() V {
	select {
	case <-d.set:
		if d.value == nil {
			var zero V
			return zero
		}
		return *d.value
	}
}

func NewDeferred[V any]() *Deferred[V] {
	return &Deferred[V]{
		set: make(chan struct{}),
	}
}

func NewDeferredWithValue[V any](v V) *Deferred[V] {
	ret := NewDeferred[V]()
	ret.SetValue(v)

	return ret
}

func Wait(d ...interface {
	Done() chan struct{}
}) {
	for _, d := range d {
		<-d.Done()
	}
}

func MapDeferred[FROM, TO any](deferred *Deferred[FROM], mapper func(FROM) TO) *Deferred[TO] {
	ret := NewDeferred[TO]()
	ret.LoadAsync(func() TO {
		return mapper(deferred.Wait())
	})
	return ret
}

func DeferMapDeferred[From, To any](deferred *Deferred[From], mapper func(From) To) *Deferred[To] {
	ret := NewDeferred[To]()
	BeforeEach(func() {
		ret.Reset()
		ret.LoadAsync(func() To {
			return mapper(deferred.Wait())
		})
	})
	return ret
}
