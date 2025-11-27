package promise_test

import (
	"context"
	"fmt"
	"time"

	// 关键：在这个目录下，必须完整引入包含子目录的路径
	"github.com/xigexb/go-promise/promise"
)

func ExampleNew() {
	// 这里模拟外部用户，所以需要使用 promise.New
	p := promise.New(func(resolve func(string), reject func(error)) {
		time.Sleep(10 * time.Millisecond)
		resolve("Hello Promise")
	})

	p.Then(func(res string) string {
		fmt.Println(res)
		return res + " World"
	}, nil)

	val, _ := p.Await(context.Background())
	fmt.Println("Result:", val)

	// Output:
	// Hello Promise
	// Result: Hello Promise
}

func ExampleAll() {
	p1 := promise.Resolve(1)
	p2 := promise.Resolve(2)

	allP := promise.All(p1, p2)

	results, _ := allP.Await(context.Background())
	fmt.Println(results)

	// Output:
	// [1 2]
}

func ExamplePromise_Timeout() {
	p := promise.New(func(resolve func(string), reject func(error)) {
		time.Sleep(1 * time.Second)
		resolve("Too slow")
	})

	_, err := p.Timeout(100*time.Millisecond, "timeout").Await(context.Background())

	if err != nil {
		fmt.Println(err.Error())
	}

	// Output:
	// timeout
}
