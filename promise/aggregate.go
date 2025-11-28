package promise

import (
	"errors"
	"sync/atomic"
)

// attachHandler 侵入式挂载回调 (Helper for Aggregators)
// 直接访问 Promise 私有字段，避免 New Promise 开销
func attachHandler[T any](p *Promise[T], handler func()) {
	if p.GetState() != Pending {
		handler()
		return
	}

	p.mu.Lock()
	if p.state != uint32(Pending) {
		p.mu.Unlock()
		handler()
	} else {
		node := getHandlerNode(handler)
		node.next = p.handlers
		p.handlers = node
		p.mu.Unlock()
	}
}

// Map 泛型转换
func Map[T any, R any](p *Promise[T], mapper func(T) (R, error)) *Promise[R] {
	return New(func(resolve func(R), reject func(error)) {
		p.Then(func(val T) T {
			if res, err := mapper(val); err != nil {
				reject(err)
			} else {
				resolve(res)
			}
			return val
		}, func(err error) error {
			reject(err)
			return err
		})
	})
}

// All 极致优化版
func All[T any](promises ...*Promise[T]) *Promise[[]T] {
	return New(func(resolve func([]T), reject func(error)) {
		count := len(promises)
		if count == 0 {
			resolve([]T{})
			return
		}

		results := make([]T, count)
		// Fix ST1023: Use short variable declaration for inferred type
		pending := int32(count)
		var doneFlag int32 = 0 // 0: running, 1: done (rejected or finished)

		for i, p := range promises {
			idx := i
			target := p

			handler := func() {
				if target.state == uint32(Fulfilled) {
					// 如果已经失败过，直接返回
					if atomic.LoadInt32(&doneFlag) == 1 {
						return
					}
					results[idx] = target.val
					// 最后一个完成
					if atomic.AddInt32(&pending, -1) == 0 {
						if atomic.CompareAndSwapInt32(&doneFlag, 0, 1) {
							resolve(results)
						}
					}
				} else {
					// 只要有一个 Rejected，整体 Rejected
					if atomic.CompareAndSwapInt32(&doneFlag, 0, 1) {
						reject(target.err)
					}
				}
			}
			attachHandler(target, handler)
		}
	})
}

// Any 极致优化版
func Any[T any](promises ...*Promise[T]) *Promise[T] {
	return New(func(resolve func(T), reject func(error)) {
		if len(promises) == 0 {
			reject(errors.New("aggregate error: no promises"))
			return
		}

		// Fix ST1023: Use short variable declaration
		pending := int32(len(promises))
		var successFlag int32 = 0

		for _, p := range promises {
			target := p
			handler := func() {
				if target.state == uint32(Fulfilled) {
					if atomic.CompareAndSwapInt32(&successFlag, 0, 1) {
						resolve(target.val)
					}
				} else {
					if atomic.AddInt32(&pending, -1) == 0 {
						if atomic.LoadInt32(&successFlag) == 0 {
							reject(errors.New("aggregate error: all promises rejected"))
						}
					}
				}
			}
			attachHandler(target, handler)
		}
	})
}

// Race 极致优化版
func Race[T any](promises ...*Promise[T]) *Promise[T] {
	return New(func(resolve func(T), reject func(error)) {
		var doneFlag int32 = 0

		for _, p := range promises {
			target := p
			handler := func() {
				if atomic.CompareAndSwapInt32(&doneFlag, 0, 1) {
					if target.state == uint32(Fulfilled) {
						resolve(target.val)
					} else {
						reject(target.err)
					}
				}
			}
			attachHandler(target, handler)
		}
	})
}

type SettledResult[T any] struct {
	Status State
	Value  T
	Reason error
}

// AllSettled 极致优化版
func AllSettled[T any](promises ...*Promise[T]) *Promise[[]SettledResult[T]] {
	return New(func(resolve func([]SettledResult[T]), reject func(error)) {
		count := len(promises)
		if count == 0 {
			resolve([]SettledResult[T]{})
			return
		}

		results := make([]SettledResult[T], count)
		// Fix ST1023: Use short variable declaration
		pending := int32(count)

		for i, p := range promises {
			idx := i
			target := p

			handler := func() {
				if target.state == uint32(Fulfilled) {
					results[idx] = SettledResult[T]{Status: Fulfilled, Value: target.val}
				} else {
					results[idx] = SettledResult[T]{Status: Rejected, Reason: target.err}
				}

				if atomic.AddInt32(&pending, -1) == 0 {
					resolve(results)
				}
			}
			attachHandler(target, handler)
		}
	})
}
