package promise

import (
	"context"
	"testing"
)

// 1. 基准：测试创建、立即 Resolve 和 Await 的极速路径 (Fast Path)
// 这个测试主要衡量对象的内存分配和无锁检查的性能
func BenchmarkFastPath(b *testing.B) {
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// 模拟最简单的同步流程
		p := Resolve(i)
		_, _ = p.Await(ctx)
	}
}

// 2. 异步：测试标准的 New -> Goroutine 执行 -> Await 流程
// 这个测试衡量 Goroutine 调度开销 + Promise 内部开销
func BenchmarkAsyncFlow(b *testing.B) {
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		p := New(func(resolve func(int), reject func(error)) {
			resolve(i)
		})
		_, _ = p.Await(ctx)
	}
}

// 3. 链式调用：测试深度链式调用的性能
// 衡量 .Then 的内存分配
func BenchmarkChaining(b *testing.B) {
	p := Resolve(0)
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// 每次构建一个 10 层深度的链
		curr := p
		for j := 0; j < 10; j++ {
			curr = curr.Then(func(val int) int {
				return val + 1
			}, nil)
		}
	}
}

// 4. 并发竞争：测试极致并发下的性能 (RunParallel)
// 这将测试 sync/atomic 和 Mutex 在多核 CPU 上的表现
func BenchmarkConcurrentResolve(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			p := New(func(resolve func(int), reject func(error)) {
				resolve(1)
			})
			// 简单的非阻塞挂载
			p.Then(func(i int) int { return i }, nil)
		}
	})
}

// 5. 聚合性能：测试 Promise.All
func BenchmarkPromiseAll(b *testing.B) {
	ctx := context.Background()
	// 准备一组 Promise
	count := 100
	tasks := make([]*Promise[int], count)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// 重新创建任务
		for k := 0; k < count; k++ {
			tasks[k] = Resolve(k)
		}

		p := All(tasks...)
		_, _ = p.Await(ctx)
	}
}

// 6. 对照组：原生 Channel 性能
// 用于对比 Promise 封装带来的额外开销（Overhead）
func BenchmarkNativeChannel(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ch := make(chan int, 1)
		go func() {
			ch <- i
			close(ch)
		}()
		<-ch
	}
}

// 7. 高级功能：测试 Map 转换性能
func BenchmarkMap(b *testing.B) {
	p := Resolve(100)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		p2 := Map(p, func(i int) (string, error) {
			return "done", nil
		})
		_, _ = p2.Await(ctx)
	}
}
