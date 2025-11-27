package promise

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// 1. 基础功能测试
func TestBasicLifecycle(t *testing.T) {
	// 直接使用 New，不需要 promise.New
	p := New(func(resolve func(string), reject func(error)) {
		time.Sleep(10 * time.Millisecond)
		resolve("hello")
	})

	val, err := p.Await(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if val != "hello" {
		t.Fatalf("expected hello, got %s", val)
	}
}

func TestReject(t *testing.T) {
	expectedErr := errors.New("fail")
	p := New(func(resolve func(int), reject func(error)) {
		reject(expectedErr)
	})

	_, err := p.Await(context.Background())
	if err != expectedErr {
		t.Fatalf("expected error %v, got %v", expectedErr, err)
	}
}

// 2. 链式调用与类型转换测试
func TestChainAndMap(t *testing.T) {
	p1 := Resolve(10) // 直接调用 Resolve

	// Int -> String
	p2 := Map(p1, func(i int) (string, error) {
		return fmt.Sprintf("val:%d", i), nil
	})

	res, err := p2.Await(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res != "val:10" {
		t.Fatalf("expected 'val:10', got '%s'", res)
	}
}

// 3. 聚合函数测试 (All, Any, AllSettled)
// 3. 聚合函数测试 (All, Any, AllSettled)
func TestAll(t *testing.T) {
	p1 := Resolve(1)
	p2 := Resolve(2)

	// 修正：使用 Map 来进行类型转换 (Delay -> int)
	// Delay 返回 Promise[struct{}]，我们需要 Promise[int] 才能放入 All
	p3 := Map(Delay(10*time.Millisecond), func(_ struct{}) (int, error) {
		return 3, nil
	})

	// 现在 p1, p2, p3 都是 *Promise[int] 类型了，可以放进 All
	all := All(p1, p2, p3)

	res, err := all.Await(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// 结果顺序对应传入顺序
	if len(res) != 3 || res[0] != 1 || res[1] != 2 || res[2] != 3 {
		t.Fatalf("unexpected result: %v", res)
	}
}

func TestAllFail(t *testing.T) {
	p1 := Resolve(1)
	p2 := Reject[int](errors.New("oops"))

	all := All(p1, p2)
	_, err := all.Await(context.Background())
	if err == nil || err.Error() != "oops" {
		t.Fatalf("expected error 'oops', got %v", err)
	}
}

func TestAllSettled(t *testing.T) {
	p1 := Resolve("ok")
	p2 := Reject[string](errors.New("bad"))

	p := AllSettled(p1, p2)
	res, _ := p.Await(context.Background())

	if len(res) != 2 {
		t.Fatal("expected 2 results")
	}
	if res[0].Status != Fulfilled || res[0].Value != "ok" {
		t.Error("p1 result mismatch")
	}
	if res[1].Status != Rejected || res[1].Reason.Error() != "bad" {
		t.Error("p2 result mismatch")
	}
}

// 4. Panic 安全测试
func TestPanicRecovery(t *testing.T) {
	p := New(func(resolve func(int), reject func(error)) {
		panic("boom")
	})

	_, err := p.Await(context.Background())
	if err == nil {
		t.Fatal("expected panic to be caught as error")
	}
	t.Logf("Caught panic: %v", err)
}

// 5. Context 与超时测试
func TestNewWithContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	p := NewWithContext(ctx, func(resolve func(int), reject func(error)) {
		time.Sleep(1 * time.Second)
		resolve(1)
	})

	cancel() // 立即取消

	_, err := p.Await(context.Background())
	if err != context.Canceled {
		t.Fatalf("expected context canceled, got %v", err)
	}
}

func TestTimeout(t *testing.T) {
	// 修正：使用 Map 将 Delay 的结果 (struct{}) 转换为 string ("done")
	p := Map(Delay(100*time.Millisecond), func(_ struct{}) (string, error) {
		return "done", nil
	})

	// 给这个 Promise[string] 加上超时限制
	pTimeout := p.Timeout(10*time.Millisecond, "too slow")

	_, err := pTimeout.Await(context.Background())
	if err == nil || err.Error() != "too slow" {
		t.Fatalf("expected timeout error, got %v", err)
	}
}

// 6. 极致并发测试
func TestConcurrencySafety(t *testing.T) {
	p := Resolve(100)
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.Then(func(i int) int {
				return i
			}, nil)
		}()
	}

	wg.Wait()

	var successCount int32
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if val, _ := p.Await(context.Background()); val == 100 {
				atomic.AddInt32(&successCount, 1)
			}
		}()
	}
	wg.Wait()

	if successCount != 100 {
		t.Errorf("expected 100 success, got %d", successCount)
	}
}
