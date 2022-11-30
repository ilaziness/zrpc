package zrpc

import (
	"go/ast"
	"log"
	"reflect"
	"sync/atomic"
)

// 实现服务注册功能，把服务器提供的方法解析成可远程调用

// methodType 方法的完整信息
type methodType struct {
	method    reflect.Method // 方法自身
	ArgType   reflect.Type   // 第一个参数的类型
	ReplyType reflect.Type   // 第二个参数的类型
	numCalls  uint64         // 调用次数，用于统计
}

func (m *methodType) NumCalls() uint64 {
	return atomic.LoadUint64(&m.numCalls)
}

// newArgv 创建参数对象
func (m *methodType) newArgv() reflect.Value {
	var argv reflect.Value
	// arg 可能是指针类型和值类型
	if m.ArgType.Kind() == reflect.Pointer {
		argv = reflect.New(m.ArgType.Elem())
	} else {
		argv = reflect.New(m.ArgType).Elem()
	}
	return argv
}

// newReplyv 创建函数返回值对象
func (m *methodType) newReplyv() reflect.Value {
	// reply 必须是指针类型
	replyv := reflect.New(m.ReplyType.Elem())
	switch m.ReplyType.Elem().Kind() {
	case reflect.Map:
		replyv.Elem().Set(reflect.MakeMap(m.ReplyType.Elem()))
	case reflect.Slice:
		replyv.Elem().Set(reflect.MakeSlice(m.ReplyType.Elem(), 0, 0))
	}
	return replyv
}

// service 服务结构体，一个服务就是对应功能struct的反射信息
type service struct {
	name   string                 //服务名称
	typ    reflect.Type           //类型
	rcvr   reflect.Value          //服务实例
	method map[string]*methodType //服务方法列表
}

// newService 新建服务器，rcvr是任意需要映射为服务的struct结构体
func newService(rcvr interface{}) *service {
	s := new(service)
	s.rcvr = reflect.ValueOf(rcvr)
	s.name = reflect.Indirect(s.rcvr).Type().Name()
	s.typ = reflect.TypeOf(rcvr)
	if !ast.IsExported(s.name) {
		log.Fatalf("rpc server: %s is not a valid service name", s.name)
	}
	s.registerMethods()
	return s
}

// registerMethods 注册结构体方法
func (s *service) registerMethods() {
	s.method = make(map[string]*methodType)
	for i := 0; i < s.typ.NumMethod(); i++ {
		method := s.typ.Method(i)
		mType := method.Type
		if mType.NumIn() != 3 || mType.NumOut() != 1 {
			continue
		}
		if mType.Out(0) != reflect.TypeOf((*error)(nil)).Elem() {
			continue
		}
		argType, replyType := mType.In(1), mType.In(2)
		if !isExportedOrBuiltinType(argType) || !isExportedOrBuiltinType(replyType) {
			continue
		}
		s.method[method.Name] = &methodType{
			method:    method,
			ArgType:   argType,
			ReplyType: replyType,
		}
		log.Printf("rpc server: register %s.%s\n", s.name, method.Name)
	}
}

func isExportedOrBuiltinType(t reflect.Type) bool {
	return ast.IsExported(t.Name()) || t.PkgPath() == ""
}

// call 根据反射值调用实际的方法
func (s *service) call(m *methodType, argv, replyv reflect.Value) error {
	atomic.AddUint64(&m.numCalls, 1)
	f := m.method.Func
	returnValues := f.Call([]reflect.Value{s.rcvr, argv, replyv})
	if errInter := returnValues[0].Interface(); errInter != nil {
		return errInter.(error)
	}
	return nil
}
