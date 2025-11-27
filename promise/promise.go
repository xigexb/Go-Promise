package promise

import (
	"context"
	"sync"
	"sync/atomic"
)

// State 枚举
type State uint32

const (
	Pending State = iota
	Fulfilled
	Rejected
)

func (s State) String() string {
	switch s {
	case Fulfilled:
		return "fulfilled"
	case Rejected:
		return "rejected"
	default:
		return "pending"
	}
}

// -------------------------------------------------------
// 极致优化 I: 使用对象池复用回调节点，消除切片扩容的 allocs
// -------------------------------------------------------

type handlerNode struct {
	fn   func()
	next *handlerNode
}

var handlerNodePool = sync.Pool{
	New: func() interface{} {
		return &handlerNode{}
	},
}

// 获取节点
func getHandlerNode(fn func()) *handlerNode {
	node := handlerNodePool.Get().(*handlerNode)
	node.fn = fn
	node.next = nil
	return node
}

// 回收链表 (Iterative to avoid recursion stack overflow)
func putHandlerChain(head *handlerNode) {
	for head != nil {
		next := head.next
		head.fn = nil // 防止内存泄漏
		head.next = nil
		handlerNodePool.Put(head)
		head = next
	}
}

// -------------------------------------------------------

type Promise[T any] struct {
	state uint32     // 原子状态
	mu    sync.Mutex // 锁

	val T
	err error

	// 优化：使用链表替代切片 []func()
	handlers *handlerNode

	signal chan struct{}
}

// New 创建 Promise
func New[T any](executor func(resolve func(T), reject func(error))) *Promise[T] {
	p := &Promise[T]{}

	GlobalDispatcher.Dispatch(func() {
		defer handlePanic(p.Reject)
		executor(p.Resolve, p.Reject)
	})

	return p
}

// NewWithContext 支持 Context
func NewWithContext[T any](ctx context.Context, executor func(resolve func(T), reject func(error))) *Promise[T] {
	p := &Promise[T]{}

	GlobalDispatcher.Dispatch(func() {
		defer handlePanic(p.Reject)
		done := make(chan struct{})

		safeResolve := func(v T) {
			select {
			case <-done:
				return
			default:
				close(done)
				p.Resolve(v)
			}
		}

		safeReject := func(e error) {
			select {
			case <-done:
				return
			default:
				close(done)
				p.Reject(e)
			}
		}

		go func() {
			select {
			case <-ctx.Done():
				safeReject(ctx.Err())
			case <-done:
				return
			}
		}()

		executor(safeResolve, safeReject)
	})

	return p
}

func (p *Promise[T]) GetState() State {
	return State(atomic.LoadUint32(&p.state))
}

// Resolve 极速路径 + 链表执行
func (p *Promise[T]) Resolve(val T) {
	if atomic.LoadUint32(&p.state) != uint32(Pending) {
		return
	}
	p.doResolve(val)
}

func (p *Promise[T]) doResolve(val T) {
	p.mu.Lock()
	if p.state != uint32(Pending) {
		p.mu.Unlock()
		return
	}

	p.val = val
	atomic.StoreUint32(&p.state, uint32(Fulfilled))

	// 截断链表，取出所有回调
	h := p.handlers
	p.handlers = nil // 释放引用

	if p.signal != nil {
		close(p.signal)
	}
	p.mu.Unlock()

	// 锁外执行回调
	p.runHandlers(h)
}

func (p *Promise[T]) Reject(err error) {
	if atomic.LoadUint32(&p.state) != uint32(Pending) {
		return
	}
	p.doReject(err)
}

func (p *Promise[T]) doReject(err error) {
	p.mu.Lock()
	if p.state != uint32(Pending) {
		p.mu.Unlock()
		return
	}

	p.err = err
	atomic.StoreUint32(&p.state, uint32(Rejected))

	h := p.handlers
	p.handlers = nil

	if p.signal != nil {
		close(p.signal)
	}
	p.mu.Unlock()

	p.runHandlers(h)
}

// runHandlers 遍历链表执行并回收
func (p *Promise[T]) runHandlers(head *handlerNode) {
	current := head
	for current != nil {
		// 捕获 panic 保护
		func(fn func()) {
			defer func() {
				if r := recover(); r != nil {
					// logs or ignore? internal callbacks should be safe
				}
			}()
			fn()
		}(current.fn)

		current = current.next
	}

	// 执行完后，归还所有节点到池中
	putHandlerChain(head)
}

// Then 优化：使用链表挂载
func (p *Promise[T]) Then(onFulfilled func(T) T, onRejected func(error) error) *Promise[T] {
	return New(func(resolve func(T), reject func(error)) {
		handle := func() {
			defer handlePanic(reject)
			currentState := p.GetState()
			if currentState == Fulfilled {
				if onFulfilled != nil {
					resolve(onFulfilled(p.val))
				} else {
					resolve(p.val)
				}
			} else if currentState == Rejected {
				if onRejected != nil {
					reject(onRejected(p.err))
				} else {
					reject(p.err)
				}
			}
		}

		if p.GetState() != Pending {
			handle()
			return
		}

		p.mu.Lock()
		if p.state != uint32(Pending) {
			p.mu.Unlock()
			handle()
		} else {
			// 优化：从池中获取节点，挂载到链表头部 (头插法效率最高)
			node := getHandlerNode(handle)
			node.next = p.handlers
			p.handlers = node
			p.mu.Unlock()
		}
	})
}

func (p *Promise[T]) Catch(onRejected func(error) error) *Promise[T] {
	return p.Then(nil, onRejected)
}

func (p *Promise[T]) Finally(onFinally func()) *Promise[T] {
	return New(func(resolve func(T), reject func(error)) {
		handle := func() {
			defer handlePanic(reject)
			onFinally()
			if p.GetState() == Fulfilled {
				resolve(p.val)
			} else {
				reject(p.err)
			}
		}

		if p.GetState() != Pending {
			handle()
			return
		}

		p.mu.Lock()
		if p.state != uint32(Pending) {
			p.mu.Unlock()
			handle()
		} else {
			node := getHandlerNode(handle)
			node.next = p.handlers
			p.handlers = node
			p.mu.Unlock()
		}
	})
}

// Await 保持不变
func (p *Promise[T]) Await(ctx context.Context) (T, error) {
	if s := p.GetState(); s == Fulfilled {
		return p.val, nil
	} else if s == Rejected {
		return *new(T), p.err
	}

	p.mu.Lock()
	if p.state == uint32(Fulfilled) {
		p.mu.Unlock()
		return p.val, nil
	}
	if p.state == uint32(Rejected) {
		p.mu.Unlock()
		return *new(T), p.err
	}

	if p.signal == nil {
		p.signal = make(chan struct{})
	}
	sig := p.signal
	p.mu.Unlock()

	select {
	case <-ctx.Done():
		return *new(T), ctx.Err()
	case <-sig:
		if p.state == uint32(Fulfilled) {
			return p.val, nil
		}
		return *new(T), p.err
	}
}
