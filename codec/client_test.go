package wyfrpc

import (
	"context"
	"net"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestClientDialTimeout 只测试客户端dial的超时
func TestClientDialTimeout(t *testing.T) {
	t.Parallel()
	listener, _ := net.Listen("tcp", ":1234")

	// 模拟 NewClient
	f := func(conn net.Conn, opt *Option) (client *Client, err error) {
		_ = conn.Close()
		time.Sleep(2 * time.Second)
		return nil, nil
	}
	t.Run("timeout", func(t *testing.T) {
		_, err := dialTimeout(f, "tcp", listener.Addr().String(), &Option{
			ConnectTimeout: time.Second,
		})
		_assert(err != nil && strings.Contains(err.Error(), "connect timeout"), "expect a timeout error")
	})
	t.Run("0", func(t *testing.T) {
		_, err := dialTimeout(f, "tcp", listener.Addr().String(), &Option{ConnectTimeout: 0})
		_assert(err == nil, "0 means no limit")
	})
}

type Bar int

// Bar.Timeout 耗时 2s
func (b Bar) Timeout(argv int, reply *int) error {
	time.Sleep(2 * time.Second)
	return nil
}

func startServer(addr chan string) {
	var b Bar
	_ = Register(&b)
	listener, _ := net.Listen("tcp", ":1235")
	addr <- listener.Addr().String()
	Accept(listener)
}

func TestClient_Call(t *testing.T) {
	t.Parallel()
	addrCh := make(chan string)
	go startServer(addrCh)
	addr := <-addrCh
	time.Sleep(time.Second)

	// 场景一：客户端设置超时时间为 1s，服务端无限制；
	t.Run("client timeout", func(t *testing.T) {
		client, _ := DialTimeout("tcp", addr)
		ctx, _ := context.WithTimeout(context.Background(), time.Second)
		var reply int
		err := client.CallTimeout(ctx, "Bar.Timeout", 999, &reply)
		_assert(err != nil && strings.Contains(err.Error(), ctx.Err().Error()), "expect a timeout error")
	})

	// 场景二，服务端设置超时时间为1s，客户端无限制
	t.Run("server handle timeout", func(t *testing.T) {
		client, _ := DialTimeout("tcp", addr, &Option{
			HandleTimeout: 1 * time.Second,
		})
		var reply int
		err := client.CallTimeout(context.Background(), "Bar.Timeout", 999, &reply)
		_assert(err != nil && strings.Contains(err.Error(), "handle timeout"), "expect a timeout error")

	})
}

// 这个测试用例使用了 unix 协议创建 socket 连接，适用于本机内部的通信，使用上与 TCP 协议并无区别
func TestXDial(t *testing.T) {
	if runtime.GOOS == "linux" {
		ch := make(chan struct{})
		addr := "/tmp/wyfrpc.sock"
		go func() {
			_ = os.Remove(addr) // 先删除一下文件，防止文件存在
			listener, err := net.Listen("unix", addr)
			if err != nil {
				t.Fatal("failed to listen unix socket")
			}
			ch <- struct{}{}
			Accept(listener) // Accept 并行处理
		}()

		<-ch

		_, err := XDial("unix@" + addr)
		_assert(err == nil, "failed to connect unix socket")
	}
}
