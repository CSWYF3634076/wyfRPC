package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
	wyfrpc "wyfRPC/codec"
	"wyfRPC/codec/xclient"
)

type Foo int

type Args struct {
	Num1, Num2 int
}

func (f Foo) Sum(args Args, reply *int) error {
	*reply = args.Num1 + args.Num2
	return nil
}

func (f Foo) Sleep(args Args, reply *int) error {
	time.Sleep(time.Second * time.Duration(args.Num1))
	*reply = args.Num1 + args.Num2
	return nil
}

func startServer(addr chan string) {
	// 注册 Foo 到 Server 中，并启动 RPC 服务
	var foo Foo
	if err := wyfrpc.Register(&foo); err != nil {
		log.Fatal("register error:", err)
	}

	// pick a free port
	listener, err := net.Listen("tcp", ":1234")
	if err != nil {
		log.Fatal("network error:", err)
	}
	log.Println("start rpc server on", listener.Addr())
	addr <- listener.Addr().String()
	wyfrpc.Accept(listener)
}

func startNewServer(addrCh chan string) {
	var foo Foo
	l, _ := net.Listen("tcp", ":0")
	server := wyfrpc.NewServer()
	_ = server.Register(&foo)
	addrCh <- l.Addr().String()
	server.Accept(l)
}

func startDebugServer(addr chan string) {
	// 注册 Foo 到 Server 中，并启动 RPC 服务
	var foo Foo

	// pick a free port
	listener, err := net.Listen("tcp", ":1235")
	if err != nil {
		log.Fatal("network error:", err)
	}
	if err := wyfrpc.Register(&foo); err != nil {
		log.Fatal("register debug error:", err)
	}
	wyfrpc.HandleHTTP()
	log.Println("start debug rpc server on", listener.Addr())
	addr <- listener.Addr().String()
	if err := http.Serve(listener, nil); err != nil {
		log.Fatal("http.Serve error:", err)
	}
}

func main() {
	log.SetFlags(0)
	addr := make(chan string)
	//go startServer(addr)

	go httpRpcCall(addr)   // http 客户端
	startDebugServer(addr) // http 服务端

	/* 负载均衡和服务发现
	log.SetFlags(0)
	ch1 := make(chan string)
	ch2 := make(chan string)
	// start two servers
	go startNewServer(ch1)
	go startNewServer(ch2)

	addr1 := <-ch1
	addr2 := <-ch2

	time.Sleep(time.Second)
	call(addr1, addr2)
	broadcast(addr1, addr2)
	*/

	/*
		day 1 client
		// in fact, following code is like a simple wyfrpc client
		conn, _ := net.Dial("tcp", <-addr)
		defer func() {
			_ = conn.Close()
		}()

		time.Sleep(time.Second)

			// send options
			_ = json.NewEncoder(conn).Encode(wyfrpc.DefaultOption)
			cc := codec.NewGobCodec(conn)
			// send request & receive response
			for i := 0; i < 5; i++ {
				h := &codec.Header{
					ServiceMethod: "Foo.Sum",
					Seq:           uint64(i),
				}
				_ = cc.Write(h, fmt.Sprintf("wyfrpc req %d", h.Seq))
				_ = cc.ReadHeader(h)
				var reply string
				_ = cc.ReadBody(&reply)
				log.Println("client:", "header:", h, " reply:", reply)
			}
	*/

}

// 将 rpc client 引入 http
func httpRpcCall(addr chan string) {

	time.Sleep(2 * time.Second)
	client, err := wyfrpc.DialHTTP("tcp", <-addr)
	defer func() { _ = client.Close() }()
	if err != nil {
		log.Fatal("wyfrpc.DialHTTP error:", err)
	}
	time.Sleep(1 * time.Second)
	// send request & receive response
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			args := &Args{Num1: i, Num2: i * i}
			var reply int
			if err := client.CallTimeout(context.Background(), "Foo.Sum", args, &reply); err != nil {
				log.Fatal("call Foo.Sum error:", err)
			}
			log.Printf("%d + %d = %d", args.Num1, args.Num2, reply)
		}(i)
	}
	wg.Wait()
}

// 开始的 rpc client ， 包括超时机制
func rpcCall(addr chan string) {
	// day2 高性能client
	client, _ := wyfrpc.DialTimeout("tcp", <-addr, &wyfrpc.Option{
		HandleTimeout: 1 * time.Second,
	})
	defer func() { _ = client.Close() }()

	time.Sleep(1 * time.Second)
	// send request & receive response
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			args := &Args{Num1: i, Num2: i * i}
			ctx, _ := context.WithTimeout(context.Background(), 3*time.Second)
			var reply int
			//if err := client.Call("Foo.Sum", args, &reply); err != nil {
			//	log.Fatal("call Foo.Sum error:", err)
			//}
			if err := client.CallTimeout(ctx, "Foo.Sum", args, &reply); err != nil {
				log.Fatal("call Foo.Sum error:", err)
			}
			log.Printf("%d + %d = %d", args.Num1, args.Num2, reply)
		}(i)
	}
	wg.Wait()
}

// 负载均衡和服务发现  下面
func foo(xc *xclient.XClient, ctx context.Context, typ, serviceMethod string, args *Args) {
	var reply int
	var err error
	switch typ {
	case "call":
		err = xc.Call(ctx, serviceMethod, args, &reply)
	case "broadcast":
		err = xc.Broadcast(ctx, serviceMethod, args, &reply)
	}
	if err != nil {
		log.Printf("%s %s error: %v", typ, serviceMethod, err)
	} else {
		log.Printf("%s %s success: %d + %d = %d", typ, serviceMethod, args.Num1, args.Num2, reply)
	}
}

func call(addr1, addr2 string) {
	d := xclient.NewMultiServerDiscovery([]string{"tcp@" + addr1, "tcp@" + addr2})
	xc := xclient.NewXClient(d, xclient.RandomSelect, nil)
	defer func() { _ = xc.Close() }()
	// send request & receive response
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			foo(xc, context.Background(), "call", "Foo.Sum", &Args{Num1: i, Num2: i * i})
		}(i)
	}
	wg.Wait()
}

func broadcast(addr1, addr2 string) {
	d := xclient.NewMultiServerDiscovery([]string{"tcp@" + addr1, "tcp@" + addr2})
	xc := xclient.NewXClient(d, xclient.RandomSelect, nil)
	defer func() { _ = xc.Close() }()
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			foo(xc, context.Background(), "broadcast", "Foo.Sum", &Args{Num1: i, Num2: i * i})
			// expect 2 - 5 timeout
			ctx, _ := context.WithTimeout(context.Background(), time.Second*2)
			foo(xc, ctx, "broadcast", "Foo.Sleep", &Args{Num1: i, Num2: i * i})
		}(i)
	}
	wg.Wait()
}
