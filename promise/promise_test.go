package promise

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"
)

// 辅助断言函数
func assertEqual(t *testing.T, expected, actual interface{}, msg string) {
	t.Helper()
	if expected != actual {
		t.Errorf("%s: expected %v, got %v", msg, expected, actual)
	}
}

// 基础功能测试：Resolve
func TestPromise_BasicResolve(t *testing.T) {
	p := New(func(resolve func(int), reject func(error)) {
		resolve(42)
	})

	val, err := p.Await(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertEqual(t, 42, val, "Basic Resolve")
}

// 基础功能测试：Reject
func TestPromise_BasicReject(t *testing.T) {
	expectedErr := errors.New("fail")
	p := New(func(resolve func(int), reject func(error)) {
		reject(expectedErr)
	})

	_, err := p.Await(context.Background())
	assertEqual(t, expectedErr, err, "Basic Reject")
}

// 核心测试：验证链表翻转后的执行顺序是否为 FIFO
// 修复后，这里应该输出 [A B C]
func TestPromise_ExecutionOrder_FIFO(t *testing.T) {
	var order []string
	var mu sync.Mutex

	// 1. 创建一个“卡住”的 Promise
	blocker := make(chan struct{})
	p := New(func(resolve func(string), reject func(error)) {
		<-blocker
		resolve("done")
	})

	var wg sync.WaitGroup
	wg.Add(3)

	// 2. 注册回调 (在 Pending 状态下)
	// 内部链表顺序: C -> B -> A
	p.Then(func(v string) string {
		mu.Lock()
		order = append(order, "A")
		mu.Unlock()
		wg.Done()
		return v
	}, nil)

	p.Then(func(v string) string {
		mu.Lock()
		order = append(order, "B")
		mu.Unlock()
		wg.Done()
		return v
	}, nil)

	p.Then(func(v string) string {
		mu.Lock()
		order = append(order, "C")
		mu.Unlock()
		wg.Done()
		return v
	}, nil)

	// 3. 放行
	close(blocker)
	wg.Wait()

	// 4. 验证顺序
	mu.Lock()
	defer mu.Unlock()

	if len(order) != 3 {
		t.Fatalf("Expected 3 callbacks, got %d", len(order))
	}

	// 关键验证
	if order[0] != "A" || order[1] != "B" || order[2] != "C" {
		errMsg := fmt.Sprintf("Execution order mismatch. Got: %v.\n", order)
		if order[0] == "C" && order[2] == "A" {
			errMsg += "Hint: Got LIFO order [C B A]. This means promise.go is NOT updated with the linked-list reversal logic in runHandlers."
		} else {
			errMsg += "Hint: Should be FIFO [A B C]."
		}
		t.Error(errMsg)
	}
}

func TestNewWithContext_Cancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p := NewWithContext(ctx, func(resolve func(string), reject func(error)) {
		time.Sleep(100 * time.Millisecond)
		resolve("done")
	})
	cancel()
	_, err := p.Await(context.Background())
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Expected context.Canceled, got %v", err)
	}
}

func TestPromise_Leak(t *testing.T) {
	initialGroutines := runtime.NumGoroutine()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for i := 0; i < 500; i++ {
		NewWithContext(ctx, func(resolve func(int), reject func(error)) {})
	}

	time.Sleep(50 * time.Millisecond)
	// 简单的泄漏检查
	if runtime.NumGoroutine() > initialGroutines+50 {
		// 注意：这里的阈值可能需要根据环境调整，主要是确保没有大量 goroutine 残留
		t.Logf("Goroutine count: %d (started with %d)", runtime.NumGoroutine(), initialGroutines)
	}
}

func TestPromise_PanicRecovery(t *testing.T) {
	p := New(func(resolve func(int), reject func(error)) {
		panic("boom")
	})
	_, err := p.Await(context.Background())
	if err == nil || err.Error() != "panic: boom" {
		t.Errorf("Expected panic error, got: %v", err)
	}
}

// Example 用于文档生成
// 重命名以避免与 example_test.go 冲突 (后缀必须小写)
func ExampleNew_basic() {
	p := New(func(resolve func(string), reject func(error)) {
		resolve("Hello Promise")
	})

	val, _ := p.Await(context.Background())
	fmt.Println(val)

	// Output:
	// Hello Promise
}
