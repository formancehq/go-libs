package collections

import (
	"reflect"
	"sync"
)

type LinkedListNode[T any] struct {
	object                 T
	list                   *LinkedList[T]
	previousNode, nextNode *LinkedListNode[T]
}

func (n *LinkedListNode[T]) Next() *LinkedListNode[T] {
	return n.nextNode
}

func (n *LinkedListNode[T]) Value() T {
	return n.object
}

func (n *LinkedListNode[T]) Remove() {
	if n == nil || n.list == nil {
		return
	}
	list := n.list
	list.mu.Lock()
	defer list.mu.Unlock()
	if n.list != list || !list.containsNodeLocked(n) {
		return
	}

	n.remove()
}

func (n *LinkedListNode[T]) remove() {
	if n.list == nil {
		return
	}
	if n.previousNode != nil {
		n.previousNode.nextNode = n.nextNode
	}
	if n.nextNode != nil {
		n.nextNode.previousNode = n.previousNode
	}
	if n == n.list.firstNode {
		n.list.firstNode = n.nextNode
	}
	if n == n.list.lastNode {
		n.list.lastNode = n.previousNode
	}
	n.previousNode = nil
	n.nextNode = nil
	n.list = nil
}

type LinkedList[T any] struct {
	mu                  sync.Mutex
	firstNode, lastNode *LinkedListNode[T]
}

func (r *LinkedList[T]) Append(objects ...T) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, object := range objects {
		if r.firstNode == nil {
			r.firstNode = &LinkedListNode[T]{
				object: object,
				list:   r,
			}
			r.lastNode = r.firstNode
			continue
		}
		r.lastNode = &LinkedListNode[T]{
			object:       object,
			previousNode: r.lastNode,
			list:         r,
		}
		r.lastNode.previousNode.nextNode = r.lastNode
	}
}

func (r *LinkedList[T]) RemoveFirst(cmp func(T) bool) *LinkedListNode[T] {
	nodes := r.nodes()
	for _, node := range nodes {
		if !cmp(node.object) {
			continue
		}

		r.mu.Lock()
		if r.containsNodeLocked(node) {
			node.remove()
			r.mu.Unlock()
			return node
		}
		r.mu.Unlock()
	}

	return nil
}

func (r *LinkedList[T]) nodes() []*LinkedListNode[T] {
	r.mu.Lock()
	defer r.mu.Unlock()

	ret := make([]*LinkedListNode[T], 0)
	node := r.firstNode
	for node != nil {
		ret = append(ret, node)
		node = node.nextNode
	}

	return ret
}

func (r *LinkedList[T]) containsNodeLocked(node *LinkedListNode[T]) bool {
	for current := r.firstNode; current != nil; current = current.nextNode {
		if current == node {
			return true
		}
	}
	return false
}

func (r *LinkedList[T]) RemoveValue(t T) *LinkedListNode[T] {
	return r.RemoveFirst(func(t2 T) bool {
		return comparableEqual(t, t2)
	})
}

func (r *LinkedList[T]) TakeFirst() T {
	r.mu.Lock()
	defer r.mu.Unlock()

	var t T
	if r.firstNode == nil {
		return t
	}
	ret := r.firstNode.object
	if r.firstNode.nextNode == nil {
		r.firstNode = nil
		r.lastNode = nil
	} else {
		r.firstNode = r.firstNode.nextNode
		r.firstNode.previousNode = nil
	}
	return ret
}

func (r *LinkedList[T]) Length() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	count := 0

	node := r.firstNode
	for node != nil {
		count++
		node = node.nextNode
	}

	return count
}

func (r *LinkedList[T]) ForEach(f func(t T)) {
	for _, t := range r.Slice() {
		f(t)
	}
}

func (r *LinkedList[T]) Slice() []T {
	r.mu.Lock()
	defer r.mu.Unlock()

	ret := make([]T, 0)
	node := r.firstNode
	for node != nil {
		ret = append(ret, node.object)
		node = node.nextNode
	}
	return ret
}

func (r *LinkedList[T]) FirstNode() *LinkedListNode[T] {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.firstNode
}

func NewLinkedList[T any]() *LinkedList[T] {
	return &LinkedList[T]{}
}

func comparableEqual[T any](a, b T) (equal bool) {
	aAny := any(a)
	bAny := any(b)

	aType := reflect.TypeOf(aAny)
	bType := reflect.TypeOf(bAny)
	if aType == nil || bType == nil {
		return aType == bType
	}
	if aType != bType || !aType.Comparable() {
		return false
	}

	defer func() {
		if recover() != nil {
			equal = false
		}
	}()

	return aAny == bAny
}
