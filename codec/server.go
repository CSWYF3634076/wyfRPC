package wyfrpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"reflect"
	"strings"
	"sync"
	"time"
	"wyfRPC/codec/codec"
)

const MagicNumber = 0x3bef5c

type Option struct {
	MagicNumber    int           // MagicNumber marks this is a wyfrpc request
	CodecType      codec.Type    // client may choose different Codec to encode body
	ConnectTimeout time.Duration // 0 means no limit
	HandleTimeout  time.Duration
}

var DefaultOption = &Option{
	MagicNumber:    MagicNumber,
	CodecType:      codec.GobType, // 默认gob
	ConnectTimeout: 10 * time.Second,
}

// Server represents an RPC Server.
type Server struct {
	serviceMap sync.Map
}

// NewServer returns a new Server.
func NewServer() *Server {
	return &Server{}
}

// DefaultServer is the default instance of *Server.
var DefaultServer = NewServer()

// Accept accepts connections on the listener and serves requests
// for each incoming connection.
func (server *Server) Accept(listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Println("rpc server: accept error:", err)
			return
		}
		go server.ServerConn(conn)
	}
}
func Accept(listener net.Listener) {
	DefaultServer.Accept(listener)
}

// ServerConn runs the server on a single connection.
// ServerConn blocks, serving the connection until the client hangs up.
func (server *Server) ServerConn(conn io.ReadWriteCloser) {
	defer func() {
		_ = conn.Close()
	}()
	var opt Option
	// TODO 这里 HandleTimeout 超时是客户端指定的，感觉不太正常
	if err := json.NewDecoder(conn).Decode(&opt); err != nil {
		log.Println("rpc server: options error: ", err)
		return
	}
	if opt.MagicNumber != MagicNumber {
		log.Printf("rpc server: invalid magic number %x", opt.MagicNumber)
		return
	}
	f := codec.NewCodecFuncMap[opt.CodecType]
	if f == nil {
		log.Printf("rpc server: invalid codec type %s", opt.CodecType)
		return
	}
	server.serverCodec(f(conn), &opt)
}

// invalidRequest is a placeholder for response argv when error occurs
var invalidRequest = struct{}{}

func (server *Server) serverCodec(cc codec.Codec, opt *Option) {
	sending := new(sync.Mutex) //  make sure to send a complete response
	wg := new(sync.WaitGroup)  // wait until all request are handled
	for {
		req, err := server.readRequest(cc)
		if err != nil {
			if req == nil {
				break // it's not possible to recover, so close the connection
			}
			req.h.Error = err.Error()
			server.sendResponse(cc, req.h, invalidRequest, sending) // 回复无效请求
			continue
		}
		wg.Add(1)
		go server.handleRequest(cc, req, sending, wg, opt.HandleTimeout)
	}
	wg.Wait()
	_ = cc.Close()
}

// request stores all information of a call
type request struct {
	h      *codec.Header // header of request
	argv   reflect.Value // argv of request
	replyv reflect.Value // replyv of request
	mtype  *methodType
	svc    *service
}

func (server *Server) readRequest(cc codec.Codec) (*request, error) {

	h, err := server.readRequestHeader(cc)
	log.Println("readRequest中的cc:", cc, " header:", h)
	if err != nil {
		return nil, err
	}
	req := &request{h: h}
	req.svc, req.mtype, err = server.findService(h.ServiceMethod)
	if err != nil {
		return req, err
	}
	req.argv = req.mtype.newArgv()
	req.replyv = req.mtype.newReplyv()

	// make sure that argvi is a pointer, ReadBody need a pointer as parameter
	argvi := req.argv.Interface()
	if req.argv.Type().Kind() != reflect.Pointer {
		// 指针的话使用地址进行赋值
		argvi = req.argv.Addr().Interface()
	}
	if err = cc.ReadBody(argvi); err != nil { // 将数据从cc中读取body信息到req的argv中
		log.Println("rpc server: read argv err:", err)
		return req, err
	}
	return req, nil
}

func (server *Server) readRequestHeader(cc codec.Codec) (*codec.Header, error) {
	var h codec.Header
	if err := cc.ReadHeader(&h); err != nil {
		if err != io.EOF && err != io.ErrUnexpectedEOF {
			log.Println("rpc server: read header error:", err)
		}
		return nil, err
	}
	return &h, nil
}

func (server *Server) handleRequest(cc codec.Codec, req *request, sending *sync.Mutex, wg *sync.WaitGroup, timeout time.Duration) {
	defer wg.Done()
	log.Println("server:", req.h, req.argv)
	called := make(chan struct{}) // 是否已经调用 “注册的服务”
	sent := make(chan struct{})   // 是否已经发送 Response
	finish := make(chan struct{}) // 用于关闭下面的协程
	defer close(finish)
	go func() {
		err := req.svc.call(req.mtype, req.argv, req.replyv)
		select {
		case <-finish:
			close(called)
			close(sent)
			// 这里不用发送回复，因为在下面的 time.After后会发送
			return
		case called <- struct{}{}:
			if err != nil {
				req.h.Error = err.Error()
				server.sendResponse(cc, req.h, invalidRequest, sending)
				sent <- struct{}{}
				return
			}
			// day 1 具体的业务逻辑，对接收到的参数如何处理
			// req.replyv = reflect.ValueOf(fmt.Sprintf("wyfrpc resp %d", req.h.Seq))
			// 正常情况 ，将rpc调用结果发送到客户端
			server.sendResponse(cc, req.h, req.replyv.Interface(), sending)
			sent <- struct{}{}
		}

	}()
	if timeout == 0 {
		<-called
		<-sent
		return
	}
	select {
	case <-time.After(timeout):
		req.h.Error = fmt.Sprintf("rpc server: request handle timeout: expect within %s", timeout)
		server.sendResponse(cc, req.h, invalidRequest, sending)
	case <-called:
		<-sent
	}
}

func (server *Server) sendResponse(cc codec.Codec, h *codec.Header, body any, sending *sync.Mutex) {
	sending.Lock()
	defer sending.Unlock()
	if err := cc.Write(h, body); err != nil {
		log.Println("rpc server: write response error:", err)
	}
}

// Register 当前类型注册到 server 的 serviceMap 中
func (server *Server) Register(rcvr any) error {
	s := newService(rcvr)
	if _, dup := server.serviceMap.LoadOrStore(s.typName, s); dup {
		return errors.New("rpc: service already defined: " + s.typName)
	}
	return nil
}

// Register publishes the receiver's methods in the DefaultServer.
func Register(rcvr any) error {
	return DefaultServer.Register(rcvr)
}

/*
因为 ServiceMethod 的构成是 “Service.Method”，
因此先将其分割成 2 部分，第一部分是 Service 的名称，
第二部分即方法名。现在 serviceMap 中找到对应的 service 实例，
再从 service 实例的 method 中，找到对应的 methodType。
*/
func (server *Server) findService(serviceMethod string) (svc *service, mtype *methodType, err error) {
	dot := strings.LastIndex(serviceMethod, ".")
	if dot < 0 {
		err = errors.New("rpc server: service/method request ill-formed: " + serviceMethod)
		return
	}
	serviceName, methodName := serviceMethod[:dot], serviceMethod[dot+1:]
	svci, ok := server.serviceMap.Load(serviceName)
	if !ok {
		err = errors.New("rpc server: can't find service " + serviceName)
		return
	}
	svc = svci.(*service)
	mtype = svc.method[methodName]
	if mtype == nil {
		err = errors.New("rpc server: can't find method " + methodName)
	}
	return
}
