package deferred

import (
	"context"
	"errors"
)

var recoverHandlers []func()

func RegisterRecoverHandler(handler func()) {
	recoverHandlers = append(recoverHandlers, handler)
}

type Deferred[V any] struct {
	value *V
	err   error
	done  chan struct{}
}

func (d *Deferred[V]) GetValue() V {
	return *d.value
}

func (d *Deferred[V]) LoadAsync(fn func() (V, error)) {
	go func() {
		defer func() {
			close(d.done)
		}()
		for _, handler := range recoverHandlers {
			defer handler()
		}

		v, err := fn()
		if err != nil {
			d.err = err
			return
		}

		d.value = &v
	}()
}

func (d *Deferred[V]) Reset() {
	if d.done != nil {
		select {
		case <-d.done:
			// already closed
		default:
			close(d.done)
		}
	}
	d.done = make(chan struct{})
	d.err = nil
	d.value = nil
}

func (d *Deferred[V]) Err() error {
	return d.err
}

func (d *Deferred[V]) Done() chan struct{} {
	return d.done
}

func (d *Deferred[V]) Wait(ctx context.Context) (V, error) {
	select {
	case <-d.done:
		var zero V
		if d.err != nil {
			return zero, d.err
		}
		if d.value == nil {
			return zero, errors.New("closed with no value")
		}

		return *d.value, nil
	case <-ctx.Done():
		var zero V
		return zero, ctx.Err()
	}
}

func (d *Deferred[V]) SetValue(v V) {
	d.value = &v
	close(d.done)
}

func (d *Deferred[V]) SetErr(err error) {
	d.err = err
	close(d.done)
}

func New[V any]() *Deferred[V] {
	return &Deferred[V]{
		done: make(chan struct{}),
	}
}

func LoadAsync[V any](fn func() (V, error)) *Deferred[V] {
	ret := New[V]()
	ret.LoadAsync(fn)
	return ret
}

func FromValue[V any](v V) *Deferred[V] {
	ret := New[V]()
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

func WaitContext(ctx context.Context, d ...interface {
	Done() chan struct{}
	Err() error
}) error {
	for _, d := range d {
		select {
		case <-d.Done():
			if d.Err() != nil {
				return d.Err()
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func WaitAndMap[FROM, TO any](deferred *Deferred[FROM], mapper func(FROM) TO) (TO, error) {
	v, err := deferred.Wait(context.Background())
	if err != nil {
		var zero TO
		return zero, err
	}

	return mapper(v), nil
}

func Map[FROM, TO any](deferred *Deferred[FROM], mapper func(FROM) TO) *Deferred[TO] {
	return LoadAsync(func() (TO, error) {
		return WaitAndMap(deferred, mapper)
	})
}
