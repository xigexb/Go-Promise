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
// 内部链表节点池化 (减少 GC 压力)
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

func getHandlerNode(fn func()) *handlerNode {
	node := handlerNodePool.Get().(*handlerNode)
	node.fn = fn
	node.next = nil
	return node
}

func putHandlerChain(head *handlerNode) {
	for head != nil {
		next := head.next
		head.fn = nil // 防止闭包引用泄漏
		head.next = nil
		handlerNodePool.Put(head)
		head = next
	}
}

// -------------------------------------------------------
// Promise 核心结构体
// -------------------------------------------------------

type Promise[T any] struct {
	val          T
	err          error
	handlers     *handlerNode // 链表头
	handlersTail *handlerNode // 链表尾 (尾插法)
	signal       chan struct{}
	mu           sync.Mutex
	state        uint32
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

// NewWithContext 包含 Context 支持
func NewWithContext[T any](ctx context.Context, executor func(resolve func(T), reject func(error))) *Promise[T] {
	p := &Promise[T]{}

	GlobalDispatcher.Dispatch(func() {
		defer handlePanic(p.Reject)

		if ctx.Err() != nil {
			p.Reject(ctx.Err())
			return
		}

		stop := context.AfterFunc(ctx, func() {
			p.Reject(ctx.Err())
		})

		safeResolve := func(v T) {
			stop()
			p.Resolve(v)
		}
		safeReject := func(e error) {
			stop()
			p.Reject(e)
		}

		executor(safeResolve, safeReject)
	})

	return p
}

func (p *Promise[T]) GetState() State {
	return State(atomic.LoadUint32(&p.state))
}

// Resolve 触发 Promise 完成
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

	h := p.handlers
	p.handlers = nil
	p.handlersTail = nil

	if p.signal != nil {
		close(p.signal)
	}
	p.mu.Unlock()

	p.runHandlers(h)
}

// Reject 触发 Promise 拒绝
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
	p.handlersTail = nil

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
		func(fn func()) {
			defer func() {
				// Fix SA9003: explicit ignore
				if r := recover(); r != nil {
					_ = r
				}
			}()
			fn()
		}(current.fn)

		current = current.next
	}
	putHandlerChain(head)
}

// Then 链式调用
// 修复核心：手动创建 child promise，并在当前 Goroutine 同步注册回调，保证顺序。
func (p *Promise[T]) Then(onFulfilled func(T) T, onRejected func(error) error) *Promise[T] {
	// 1. 手动创建 Child Promise (不通过 New 启动 Goroutine)
	child := &Promise[T]{}

	// 2. 定义处理逻辑 (闭包捕获 child)
	handle := func() {
		defer handlePanic(child.Reject)

		// Fix QF1003: Use switch for state check
		switch p.GetState() {
		case Fulfilled:
			if onFulfilled != nil {
				res := onFulfilled(p.val)
				child.Resolve(res)
			} else {
				child.Resolve(p.val)
			}
		case Rejected:
			if onRejected != nil {
				err := onRejected(p.err)
				// 注意：在当前实现中，Catch 返回的是 error，所以继续 Reject
				child.Reject(err)
			} else {
				child.Reject(p.err)
			}
		}
	}

	// 3. 同步注册 (Synchronous Registration)
	// 只有这样才能保证 TestPromise_ExecutionOrder_FIFO 中的调用顺序
	if p.GetState() != Pending {
		GlobalDispatcher.Dispatch(handle)
	} else {
		p.mu.Lock()
		if p.state != uint32(Pending) {
			p.mu.Unlock()
			GlobalDispatcher.Dispatch(handle)
		} else {
			// 尾插法
			node := getHandlerNode(handle)
			if p.handlers == nil {
				p.handlers = node
				p.handlersTail = node
			} else {
				p.handlersTail.next = node
				p.handlersTail = node
			}
			p.mu.Unlock()
		}
	}

	return child
}

func (p *Promise[T]) Catch(onRejected func(error) error) *Promise[T] {
	return p.Then(nil, onRejected)
}

// Finally 链式调用
func (p *Promise[T]) Finally(onFinally func()) *Promise[T] {
	// 1. 手动创建 Child Promise
	child := &Promise[T]{}

	// 2. 定义处理逻辑
	handle := func() {
		defer handlePanic(child.Reject)
		onFinally()
		// Finally 不改变结果，除非 Panic
		if p.GetState() == Fulfilled {
			child.Resolve(p.val)
		} else {
			child.Reject(p.err)
		}
	}

	// 3. 同步注册
	if p.GetState() != Pending {
		GlobalDispatcher.Dispatch(handle)
	} else {
		p.mu.Lock()
		if p.state != uint32(Pending) {
			p.mu.Unlock()
			GlobalDispatcher.Dispatch(handle)
		} else {
			// 尾插法
			node := getHandlerNode(handle)
			if p.handlers == nil {
				p.handlers = node
				p.handlersTail = node
			} else {
				p.handlersTail.next = node
				p.handlersTail = node
			}
			p.mu.Unlock()
		}
	}

	return child
}

// Await 阻塞等待结果
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
		if p.GetState() == Fulfilled {
			return p.val, nil
		}
		return *new(T), p.err
	}
}
