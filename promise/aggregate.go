package promise

import (
	"errors"
	"sync/atomic"
)

// Map 必须创建新 Promise，无法省略
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

// -------------------------------------------------------
// 极致优化 II: 侵入式聚合 (Intrusive Aggregation)
// 直接挂载回调到目标 Promise 的链表，跳过 .Then() 的中间 Promise 创建
// -------------------------------------------------------

// All 极致优化版
func All[T any](promises ...*Promise[T]) *Promise[[]T] {
	return New(func(resolve func([]T), reject func(error)) {
		count := len(promises)
		if count == 0 {
			resolve([]T{})
			return
		}

		results := make([]T, count)
		var pending int32 = int32(count)
		var doneFlag int32 = 0

		for i, p := range promises {
			// 捕获变量
			idx := i
			target := p

			// 定义轻量回调 (Closure only, no Promise struct alloc)
			handler := func() {
				// 直接访问 target 的内部状态
				// 注意：进入此回调时，target 必定已经完成
				if target.state == uint32(Fulfilled) {
					// 快速检查是否已经失败过
					if atomic.LoadInt32(&doneFlag) == 1 {
						return
					}

					results[idx] = target.val // 直接读取，无类型转换开销

					if atomic.AddInt32(&pending, -1) == 0 {
						if atomic.CompareAndSwapInt32(&doneFlag, 0, 1) {
							resolve(results)
						}
					}
				} else {
					// Rejected
					if atomic.CompareAndSwapInt32(&doneFlag, 0, 1) {
						reject(target.err)
					}
				}
			}

			// 1. 快速路径：如果已经完成，直接执行
			if target.GetState() != Pending {
				handler()
				continue
			}

			// 2. 慢路径：侵入式挂载 (不调用 Then)
			target.mu.Lock()
			if target.state != uint32(Pending) {
				target.mu.Unlock()
				handler()
			} else {
				// 从池中获取节点，手动挂载
				node := getHandlerNode(handler)
				node.next = target.handlers
				target.handlers = node
				target.mu.Unlock()
			}
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

		var pending int32 = int32(len(promises))
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

			if target.GetState() != Pending {
				handler()
				continue
			}

			target.mu.Lock()
			if target.state != uint32(Pending) {
				target.mu.Unlock()
				handler()
			} else {
				node := getHandlerNode(handler)
				node.next = target.handlers
				target.handlers = node
				target.mu.Unlock()
			}
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

			if target.GetState() != Pending {
				handler()
				continue
			}

			target.mu.Lock()
			if target.state != uint32(Pending) {
				target.mu.Unlock()
				handler()
			} else {
				node := getHandlerNode(handler)
				node.next = target.handlers
				target.handlers = node
				target.mu.Unlock()
			}
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
		var pending int32 = int32(count)

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

			if target.GetState() != Pending {
				handler()
				continue
			}

			target.mu.Lock()
			if target.state != uint32(Pending) {
				target.mu.Unlock()
				handler()
			} else {
				node := getHandlerNode(handler)
				node.next = target.handlers
				target.handlers = node
				target.mu.Unlock()
			}
		}
	})
}
