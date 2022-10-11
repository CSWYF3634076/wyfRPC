package wyfrpc

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"reflect"
	"sync"
	"wyfRPC/codec/codec"
)

const MagicNumber = 0x3bef5c

type Option struct {
	MagicNumber int        // MagicNumber marks this is a wyfrpc request
	CodecType   codec.Type // client may choose different Codec to encode body
}

var DefaultOption = &Option{
	MagicNumber: MagicNumber,
	CodecType:   codec.GobType, // 默认gob
}

// Server represents an RPC Server.
type Server struct{}

// NewServer returns a new Server.
func NewServer() *Server {
	return &Server{}
}

// DefaultServer is the default instance of *Server.
var DefaultServer = NewServer()

// Accept accepts connections on the listener and serves requests
// for each incoming connection.
func (server Server) Accept(listener net.Listener) {
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
func (server Server) ServerConn(conn io.ReadWriteCloser) {
	defer func() {
		_ = conn.Close()
	}()
	var opt Option
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
	server.serverCodec(f(conn))
}

// invalidRequest is a placeholder for response argv when error occurs
var invalidRequest = struct{}{}

func (server Server) serverCodec(cc codec.Codec) {
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
		go server.handleRequest(cc, req, sending, wg)
	}
	wg.Wait()
	_ = cc.Close()
}

// request stores all information of a call
type request struct {
	h      *codec.Header // header of request
	argv   reflect.Value // argv of request
	replyv reflect.Value // replyv of request
}

func (server Server) readRequest(cc codec.Codec) (*request, error) {
	h, err := server.readRequestHeader(cc)
	if err != nil {
		return nil, err
	}
	req := &request{h: h}
	// TODO: now we don't know the type of request argv
	// day 1, just suppose it's string
	req.argv = reflect.New(reflect.TypeOf(""))               // 初始化参数为空
	if err = cc.ReadBody(req.argv.Interface()); err != nil { // 将数据写入从cc中读取body信息到req的argv中
		log.Println("rpc server: read argv err:", err)
	}
	return req, nil
}

func (server Server) readRequestHeader(cc codec.Codec) (*codec.Header, error) {
	var h codec.Header
	if err := cc.ReadHeader(&h); err != nil {
		if err != io.EOF && err != io.ErrUnexpectedEOF {
			log.Println("rpc server: read header error:", err)
		}
		return nil, err
	}
	return &h, nil
}

func (server Server) handleRequest(cc codec.Codec, req *request, sending *sync.Mutex, wg *sync.WaitGroup) {
	defer wg.Done()
	log.Println("server:", req.h, req.argv.Elem())

	// 具体的业务逻辑，对接收到的参数如何处理
	req.replyv = reflect.ValueOf(fmt.Sprintf("wyfrpc resp %d", req.h.Seq))

	server.sendResponse(cc, req.h, req.replyv.Interface(), sending)
}

func (server Server) sendResponse(cc codec.Codec, h *codec.Header, body any, sending *sync.Mutex) {
	sending.Lock()
	defer sending.Unlock()
	if err := cc.Write(h, body); err != nil {
		log.Println("rpc server: write response error:", err)
	}
}
