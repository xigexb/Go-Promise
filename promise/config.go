package promise

import (
	"fmt"
)

// TaskDispatcher 定义任务调度器接口
// 高并发场景下，建议使用协程池（如 ants）实现此接口以复用 Goroutine
type TaskDispatcher interface {
	Dispatch(func())
}

// defaultDispatcher 默认使用原生 Goroutine，适合大多数场景
type defaultDispatcher struct{}

func (d *defaultDispatcher) Dispatch(f func()) {
	go f()
}

var (
	// GlobalDispatcher 全局调度器
	GlobalDispatcher TaskDispatcher = &defaultDispatcher{}
)

// SetDispatcher 允许替换全局调度器
func SetDispatcher(d TaskDispatcher) {
	GlobalDispatcher = d
}

// handlePanic 统一的 Panic 恢复逻辑，防止 Goroutine 崩溃导致进程退出
func handlePanic(reject func(error)) {
	if r := recover(); r != nil {
		err, ok := r.(error)
		if !ok {
			err = fmt.Errorf("panic: %v", r)
		}
		reject(err)
	}
}
