package promise

import (
	"fmt"
)

// TaskDispatcher 定义任务调度器接口
// 高并发场景下，建议通过 SetDispatcher 注入协程池（如 ants）以复用 Goroutine
type TaskDispatcher interface {
	Dispatch(func())
}

// defaultDispatcher 默认使用原生 Goroutine
type defaultDispatcher struct{}

func (d *defaultDispatcher) Dispatch(f func()) {
	go f()
}

var (
	// GlobalDispatcher 全局调度器，默认为原生 go func
	GlobalDispatcher TaskDispatcher = &defaultDispatcher{}
)

// SetDispatcher 允许替换全局调度器 (例如注入 ants)
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
		// 确保 reject 存在
		if reject != nil {
			reject(err)
		}
	}
}
