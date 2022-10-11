package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"time"
	wyfrpc "wyfRPC/codec"
	"wyfRPC/codec/codec"
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
	addr := make(chan string)
	go startServer(addr)

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

}
