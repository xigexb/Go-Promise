package promise

import (
	"errors"
	"time"
)

// Resolve 静态方法
func Resolve[T any](val T) *Promise[T] {
	return &Promise[T]{
		state: uint32(Fulfilled),
		val:   val,
	}
}

// Reject 静态方法
func Reject[T any](err error) *Promise[T] {
	return &Promise[T]{
		state: uint32(Rejected),
		err:   err,
	}
}

// Delay 延迟 Promise
func Delay(d time.Duration) *Promise[struct{}] {
	return New(func(resolve func(struct{}), reject func(error)) {
		time.AfterFunc(d, func() {
			resolve(struct{}{})
		})
	})
}

// Timeout 超时控制
func (p *Promise[T]) Timeout(d time.Duration, msg string) *Promise[T] {
	return New(func(resolve func(T), reject func(error)) {
		timer := time.NewTimer(d)
		defer timer.Stop() // 确保 timer 资源释放

		done := make(chan struct{})

		p.Then(func(val T) T {
			select {
			case done <- struct{}{}:
				resolve(val)
			default:
			}
			return val
		}, func(err error) error {
			select {
			case done <- struct{}{}:
				reject(err)
			default:
			}
			return err
		})

		select {
		case <-done:
		case <-timer.C:
			errMsg := "promise timeout"
			if msg != "" {
				errMsg = msg
			}
			reject(errors.New(errMsg))
		}
	})
}

// Tap 副作用钩子 (不改变值)
func (p *Promise[T]) Tap(onTap func(val T, err error)) *Promise[T] {
	return p.Then(func(val T) T {
		onTap(val, nil)
		return val
	}, func(err error) error {
		onTap(*new(T), err)
		return err
	})
}

// Promisify 将标准 Go 函数转为 Promise
func Promisify[T any](f func() (T, error)) *Promise[T] {
	return New(func(resolve func(T), reject func(error)) {
		val, err := f()
		if err != nil {
			reject(err)
		} else {
			resolve(val)
		}
	})
}
