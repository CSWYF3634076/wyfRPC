package main

import (
	"fmt"
	"log"
	"net"
	"sync"
	"time"
	wyfrpc "wyfRPC/codec"
)

func startServer(addr chan string) {
	// pick a free port
	listener, err := net.Listen("tcp", ":1234")
	if err != nil {
		log.Fatal("network error:", err)
	}
	log.Println("start rpc server on", listener.Addr())
	addr <- listener.Addr().String()
	wyfrpc.Accept(listener)
}

func main() {
	log.SetFlags(0)
	addr := make(chan string)
	go startServer(addr)

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

	// day2 高性能client
	client, _ := wyfrpc.Dial("tcp", <-addr)
	defer func() { _ = client.Close() }()

	time.Sleep(1 * time.Second)
	// send request & receive response
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			args := fmt.Sprintf("wyfrpc req args %d", i)
			var reply string
			if err := client.Call("Foo.Sum", args, &reply); err != nil {
				log.Fatal("call Foo.Sum error:", err)
			}
			log.Println("reply :", reply)
		}(i)
	}
	wg.Wait()

}
