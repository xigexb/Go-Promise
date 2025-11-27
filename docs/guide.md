# go-promise 深度开发指南

本教程将引导你从零开始掌握 `go-promise`，利用 Go 1.18+ 泛型特性编写高性能、类型安全且易于维护的异步代码。

## 目录

1. [环境准备](#1-环境准备)
2. [Hello Promise：你的第一个异步任务](#2-hello-promise你的第一个异步任务)
3. [链式调用与数据流转换 (Chaining & Map)](#3-链式调用与数据流转换-chaining--map)
4. [错误处理最佳实践](#4-错误处理最佳实践)
5. [并发编排：同时处理多个任务](#5-并发编排同时处理多个任务)
6. [Context 集成与超时控制](#6-context-集成与超时控制)
7. [高级技巧：自定义调度器与 Panic 防护](#7-高级技巧自定义调度器与-panic-防护)

---

## 1. 环境准备

确保你的 Go 版本 >= 1.18。

```bash
go get github.com/xigexb/go-promise
```

在代码中引入：

```go
import (
"github.com/xigexb/go-promise/promise"
)
```

---

## 2. Hello Promise：你的第一个异步任务

传统的 Goroutine + Channel 写法往往需要手动管理通道关闭和错误传递。使用 Promise，我们可以更直观地定义“任务”。

### 2.1 创建任务 (`New`)

```go
func main() {
// 泛型 T 指定了返回值的类型，这里是 string
p := promise.New(func (resolve func(string), reject func (error)) {
// 模拟耗时操作
time.Sleep(100 * time.Millisecond)

// 成功时调用 resolve
resolve("World")

// 失败时调用 reject(err)
})

// 获取结果
result, err := p.Await(context.Background())
if err != nil {
log.Fatal(err)
}
fmt.Printf("Hello %s\n", result)
}
```

### 2.2 快速创建 (`Resolve`/`Reject`)

如果你已经有了结果，或者需要为了接口一致性返回一个 Promise：

```go
// 立即成功的 Promise
pSuccess := promise.Resolve(100)

// 立即失败的 Promise
pFail := promise.Reject[int](errors.New("invalid id"))
```

---

## 3. 链式调用与数据流转换 (Chaining & Map)

Promise 的核心在于解决“回调地狱”。我们可以将多个步骤串联起来。

### 3.1 使用 `Then` 串联逻辑

```go
promise.Resolve(10).
Then(func (val int) int {
// 步骤 1: 加倍
return val * 2
}, nil).
Then(func (val int) int {
// 步骤 2: 打印
fmt.Println("Current:", val)
return val
}, nil)
```

### 3.2 使用 `Map` 转换类型

Go 是强类型语言，`Then` 只能返回相同类型。如果需要将 `int` 转为 `string`，请使用 `Map`。

```go
func GetUserToken(userID int) *promise.Promise[string] {
// 1. 获取用户 ID (Promise[int])
pID := promise.Resolve(userID)

// 2. 将 ID 转换为 Token (int -> string)
// Map 函数会自动处理上游的异常，如果 pID 失败，这里不会执行
return promise.Map(pID, func (id int) (string, error) {
return fmt.Sprintf("TOKEN_%d_SECRET", id), nil
})
}
```

---

## 4. 错误处理最佳实践

Promise 的错误具有**冒泡**特性。你不需要在每一步都检查错误，只需在最后捕获一次。

### 4.1 统一捕获 (`Catch`)

```go
promise.New(func (resolve func (int), reject func (error)) {
// 模拟数据库错误
reject(errors.New("db connection failed"))
}).
Then(func (i int) int {
// 这一步会被跳过
return i + 1
}, nil).
Catch(func (err error) error {
// 在这里统一处理错误
log.Printf("Task failed: %v", err)
// 可以吞掉错误（返回 nil），或者继续抛出
return err
})
```

### 4.2 资源清理 (`Finally`)

无论成功还是失败，`Finally` 都会执行。常用于关闭连接或释放锁。

```go
p.Finally(func () {
fmt.Println("Cleanup resources...")
})
```

---

## 5. 并发编排：同时处理多个任务

这是 `go-promise` 性能最强悍的部分。

### 5.1 `Promise.All` (全成功或一失败)

适用于：批量查询、数据聚合。

```go
func BatchFetch() {
var tasks []*promise.Promise[int]

// 准备 10 个并发任务
for i := 0; i < 10; i++ {
idx := i
tasks = append(tasks, promise.New(func (resolve func (int), reject func (error)) {
time.Sleep(10 * time.Millisecond)
resolve(idx)
}))
}

// 并行执行，等待全部完成
// 泛型自动推断为 []int
allP := promise.All(tasks...)

results, err := allP.Await(context.Background())
if err != nil {
fmt.Println("One of the tasks failed")
return
}
fmt.Println("All done:", results)
}
```

### 5.2 `Promise.Race` (最快胜出)

适用于：多节点请求，取最快响应。

```go
p1 := promise.Delay(1 * time.Second) // 慢
p2 := promise.Delay(50 * time.Millisecond) // 快

// 谁先完成就返回谁的结果，另一个会被忽略
winner := promise.Race(p1, p2)
```

### 5.3 `Promise.AllSettled` (无论成败)

适用于：即便部分任务失败，也需要获取其他成功任务结果的场景。

```go
p1 := promise.Resolve("ok")
p2 := promise.Reject[string](errors.New("fail"))

results, _ := promise.AllSettled(p1, p2).Await(context.Background())

for _, res := range results {
if res.Status == promise.Fulfilled {
fmt.Println("Success:", res.Value)
} else {
fmt.Println("Error:", res.Reason)
}
}
```

---

## 6. Context 集成与超时控制

在 Go 的微服务开发中，Context 是核心。`go-promise` 提供了原生支持。

### 6.1 响应 Context 取消

使用 `NewWithContext` 创建的任务，一旦外部 Context 被 Cancel，任务会立即收到通知（如果 Executor 内部处理了 Done 信号）。

```go
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()

p := promise.NewWithContext(ctx, func (resolve func (string), reject func (error)) {
// 模拟长任务
select {
case <-time.After(5 * time.Second):
resolve("done")
case <-ctx.Done():
// Context 超时或取消会自动触发 Reject，这里只需处理清理工作
fmt.Println("Task canceled by context")
}
})

_, err := p.Await(context.Background())
// err 将是 context.DeadlineExceeded
```

### 6.2 单独设置超时 (`Timeout`)

如果你不想传递 Context，也可以直接给 Promise 设限：

```go
p := promise.New(...) // 一个可能永远卡住的任务

// 如果 100ms 内没结果，强制返回 timeout error
result, err := p.Timeout(100 * time.Millisecond, "request timeout").Await(ctx)
```

---

## 7. 高级技巧：自定义调度器与 Panic 防护

### 7.1 Panic 自动恢复

库内部自动集成了 Panic Recover。你不需要在每个 Goroutine 里写 `recover()`。

```go
p := promise.New(func (resolve func (int), reject func (error)) {
panic("something went wrong!")
})

_, err := p.Await(ctx)
fmt.Println(err) // Output: panic: something went wrong!
```

### 7.2 对接协程池 (Ants 等)

在高并发场景（如每秒 10万+ 请求）下，频繁创建 Goroutine 会导致性能下降。你可以通过实现 `TaskDispatcher` 接口来接管 Goroutine
的创建。

```go
// 1. 定义适配器
type AntsDispatcher struct {
pool *ants.Pool
}

func (d *AntsDispatcher) Dispatch(f func ()) {
_ = d.pool.Submit(f)
}

// 2. 初始化时设置
func init() {
pool, _ := ants.NewPool(10000)
promise.SetDispatcher(&AntsDispatcher{pool: pool})
}
```

这样，所有 `promise.New` 产生的任务都会被提交到协程池中执行，极大地降低资源消耗。

---

> **结语**
>
> `go-promise` 的设计哲学是：**像写同步代码一样写异步逻辑**，同时不牺牲 Go 语言原本的高性能特性。希望本指南能帮助你在项目中更高效地使用它。
