package promise

import (
	"context"
	"testing"
)

// 1. 基准：最简单的 Resolve
func BenchmarkPromise_Resolve(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		p := New(func(resolve func(int), reject func(error)) {
			resolve(i)
		})
		_, _ = p.Await(context.Background())
	}
}

// 2. 基准：链式调用 (测试链表节点池化和锁开销)
func BenchmarkPromise_Chain_Deep(b *testing.B) {
	p := Resolve(0)
	b.ReportAllocs()
	b.ResetTimer()

	// 链式调用 1000 次 Then
	for i := 0; i < b.N; i++ {
		// 注意：这里测试的是构建链条的速度
		p.Then(func(v int) int {
			return v + 1
		}, nil)
	}
}

// 3. 性能关键点：测试 NewWithContext 的开销
// 重点验证我们用 AfterFunc 替代 Go Routine 后的效果
func BenchmarkNewWithContext_Overhead(b *testing.B) {
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		p := NewWithContext(ctx, func(resolve func(int), reject func(error)) {
			resolve(i)
		})
		_, _ = p.Await(context.Background())
	}
}

// 4. 并发竞争测试
func BenchmarkPromise_Parallel_Resolve(b *testing.B) {
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			p := New(func(resolve func(int), reject func(error)) {
				resolve(1)
			})
			p.Then(func(i int) int { return i }, nil)
		}
	})
}

// 5. 聚合性能测试 (All) - 验证侵入式聚合的性能
func BenchmarkPromise_All_100(b *testing.B) {
	b.ReportAllocs()

	// 预先创建好的 dummy promises
	promises := make([]*Promise[int], 100)
	for i := 0; i < 100; i++ {
		promises[i] = Resolve(i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// 由于 promises 已经 Resolved，All 会走快速路径
		// 这测试了 All 内部原子操作和内存分配的极限速度
		p := All(promises...)
		_, _ = p.Await(context.Background())
	}
}

// 6. 真实场景模拟：短任务异步执行
func BenchmarkPromise_Async_ShortTask(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		p := New(func(resolve func(int), reject func(error)) {
			// 模拟一个极短的异步操作 (比如内存查找)
			go func() {
				resolve(1)
			}()
		})
		_, _ = p.Await(context.Background())
	}
}
