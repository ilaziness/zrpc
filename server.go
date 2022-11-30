// server
package zrpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/ilaziness/zrpc/codec"
)

// Option 连接服务器的协商字段
// 连接服务器后，服务器读取出option的内容，根据option的内容处理后续的数据
// | Option | Header1 | Body1 | Header2 | Body2 | ...
// 如上，一次tcp连接，Option协商数据内容信息，一对heade、body代表一次tpc方法调用，可以不断的发送rpc方法调用
type Option struct {
	CodecType      codec.Type // 数据编码类型
	ConnectTimeout time.Duration
	HandleTimeout  time.Duration
}

// DefaultOption 默认option
var DefaultOption = &Option{
	CodecType:      codec.GobType,
	ConnectTimeout: time.Second * 10,
}

// DefaultServer 默认服务器对象
var DefaultServer = NewServer()

func Accept(lis net.Listener) { DefaultServer.Accept(lis) }

// Server 逻辑实现
type Server struct {
	serviceMap sync.Map //服务器提供的服务列表，key是服务器名称
}

func NewServer() *Server {
	return &Server{}
}

// Register 注册服务
func (server *Server) Register(rcvr interface{}) error {
	s := newService(rcvr)
	if _, dup := server.serviceMap.LoadOrStore(s.name, s); dup {
		return errors.New("rpc: service already defined: " + s.name)
	}
	return nil
}

// Register 快捷注册方法，创建默认服务器注册服务
func Register(rcvr interface{}) error { return DefaultServer.Register(rcvr) }

// findService 查找服务
func (server *Server) findService(serviceMethod string) (svc *service, mtype *methodType, err error) {
	dot := strings.LastIndex(serviceMethod, ".")
	if dot < 0 {
		err = errors.New("rpc server: service/method request ill-formed: " + serviceMethod)
		return
	}
	// 服务器列表里面找到对应的服务
	serviceName, methodName := serviceMethod[:dot], serviceMethod[dot+1:]
	svci, ok := server.serviceMap.Load(serviceName)
	if !ok {
		err = errors.New("rpc server: can't find service " + serviceName)
		return
	}

	// 拿到服务本身和方法
	svc = svci.(*service)
	mtype = svc.method[methodName]
	if mtype == nil {
		err = errors.New("rpc server: can't find method " + methodName)
	}
	return
}

// Accept 接受连接，然后交给ServeConn处理
func (server *Server) Accept(lis net.Listener) {
	for {
		conn, err := lis.Accept()
		if err != nil {
			log.Println("rpc server: accept error:", err)
			return
		}
		go server.ServeConn(conn)
	}
}

// ServeConn 处理连接
func (server *Server) ServeConn(conn io.ReadWriteCloser) {
	defer func() {
		_ = conn.Close()
	}()

	// 反序列化option，option json格式
	var opt Option
	if err := json.NewDecoder(conn).Decode(&opt); err != nil {
		log.Println("rpc server: options error: ", err)
		return
	}
	log.Println("option:", opt)

	// 根据option的CodecType得到对应的编解码器
	f := codec.NewCodecFuncMap[opt.CodecType]
	if f == nil {
		log.Printf("rpc server: invalid codec type %s", opt.CodecType)
		return
	}
	// 实例化编码器后，交给serveCodec处理数据
	server.serveCodec(f(conn))
}

var invalidRequest = struct{}{}

// serveCodec 处理数据
func (server *Server) serveCodec(cc codec.Codec) {
	sending := new(sync.Mutex)
	wg := new(sync.WaitGroup)
	// 一次连接可以处理器多个请求
	for {
		// 读取rpc请求
		req, err := server.readRequest(cc)
		if err != nil {
			// 出错后结束请求
			if req == nil {
				break // it's not possible to recover, so close the connection
			}
			req.h.Error = err.Error()
			//发送无效请求响应
			server.sendResponse(cc, req.h, invalidRequest, sending)
			continue
		}
		wg.Add(1)
		// 处理请求
		go server.handleRequest(cc, req, sending, wg, DefaultOption.HandleTimeout)
	}
	wg.Wait()
	_ = cc.Close()
}

// request 代表一次rpc 方法请求
type request struct {
	h            *codec.Header
	argv, replyv reflect.Value //参数和响应
	mtype        *methodType
	svc          *service
}

// readRequestHeader 读取请求header
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

// readRequest 读取rpc请求
func (server *Server) readRequest(cc codec.Codec) (*request, error) {
	h, err := server.readRequestHeader(cc)
	if err != nil {
		return nil, err
	}
	log.Println("rpc server header:", h)
	req := &request{h: h}
	//找到请求对应的服务
	req.svc, req.mtype, err = server.findService(h.ServiceMethod)
	if err != nil {
		return req, err
	}
	req.argv = req.mtype.newArgv()
	req.replyv = req.mtype.newReplyv()

	// 确保 argvi 是指针类型， ReadBody需要指针类型参数
	argvi := req.argv.Interface()
	if req.argv.Type().Kind() != reflect.Pointer {
		argvi = req.argv.Addr().Interface()
	}

	// 读取参数
	if err = cc.ReadBody(argvi); err != nil {
		log.Println("rpc server: read argv err:", err)
	}
	return req, nil
}

// sendResponse 发送结果
func (server *Server) sendResponse(cc codec.Codec, h *codec.Header, body interface{}, sending *sync.Mutex) {
	sending.Lock()
	defer sending.Unlock()
	if err := cc.Write(h, body); err != nil {
		log.Println("rpc server: write response error:", err)
	}
}

// handleRequest 处理请求
func (server *Server) handleRequest(cc codec.Codec, req *request, sending *sync.Mutex, wg *sync.WaitGroup, timeout time.Duration) {
	defer wg.Done()
	called := make(chan int)
	sent := make(chan int)
	// 调用方法
	go func() {
		err := req.svc.call(req.mtype, req.argv, req.replyv)
		called <- 1
		if err != nil {
			req.h.Error = err.Error()
			server.sendResponse(cc, req.h, invalidRequest, sending)
			sent <- 1
			return
		}
		// 返回响应结果
		server.sendResponse(cc, req.h, req.replyv.Interface(), sending)
		sent <- 1
	}()

	// 超时控制
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

const (
	connected        = "200 Connected to Gee RPC"
	defaultRPCPath   = "/_zprc_"
	defaultDebugPath = "/debug/zrpc"
)

// ServeHTTP 实现 http.Handler 接口， 处理RPC请求.
func (server *Server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != "CONNECT" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = io.WriteString(w, "405 must CONNECT\n")
		return
	}
	// w断言成Hijacker类型，调用Hijack接管tcp连接，之后http所在的tcp连接将由调用者来控制
	conn, _, err := w.(http.Hijacker).Hijack()
	if err != nil {
		log.Print("rpc hijacking ", req.RemoteAddr, ": ", err.Error())
		return
	}
	_, _ = io.WriteString(conn, "HTTP/1.0 "+connected+"\n\n")
	server.ServeConn(conn)
}

// HandleHTTP registers an HTTP handler for RPC messages on rpcPath.
// It is still necessary to invoke http.Serve(), typically in a go statement.
func (server *Server) HandleHTTP() {
	http.Handle(defaultRPCPath, server)
	http.Handle(defaultDebugPath, debugHTTP{server})
	log.Println("rpc server debug path:", defaultDebugPath)
}

// HandleHTTP is a convenient approach for default server to register HTTP handlers
func HandleHTTP() {
	DefaultServer.HandleHTTP()
}
