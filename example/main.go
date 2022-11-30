package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/ilaziness/zrpc"
)

// 定义一个Foo服务
type Foo int

type Args struct{ Num1, Num2 int }

func (f Foo) Sum(args Args, reply *int) error {
	*reply = args.Num1 + args.Num2
	return nil
}

/////////////////

// startServer 注册服务，启动服务器
func startServer(addr chan string) {
	var foo Foo
	if err := zrpc.Register(&foo); err != nil {
		log.Fatal("register error:", err)
	}
	// pick a free port
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		log.Fatal("network error:", err)
	}
	log.Println("start rpc server on", l.Addr())
	addr <- l.Addr().String()
	zrpc.Accept(l)
}

func startServer2(addrCh chan string) {
	var foo Foo
	l, _ := net.Listen("tcp", ":9999")
	_ = zrpc.Register(&foo)
	zrpc.HandleHTTP()
	addrCh <- l.Addr().String()
	_ = http.Serve(l, nil)
}

func main() {
	log.SetFlags(0)
	addr := make(chan string)
	go startServer2(addr)
	//client, _ := zrpc.Dial("tcp", <-addr)
	addrStr := <-addr
	client, _ := zrpc.DialHTTP("tcp", addrStr)
	log.Println(addrStr)
	defer func() { _ = client.Close() }()

	time.Sleep(time.Second)
	// 发送请求和接收响应
	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			args := &Args{Num1: i, Num2: i * i}
			var reply int
			if err := client.Call(ctx, "Foo.Sum", args, &reply); err != nil {
				log.Println("call Foo.Sum error:", err)
				return
			}
			log.Printf("%d + %d = %d", args.Num1, args.Num2, reply)
		}(i)
	}
	wg.Wait()
	time.Sleep(time.Minute)
}
