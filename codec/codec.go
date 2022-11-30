package codec

import (
	"io"
)

// Header 请求头信息
type Header struct {
	ServiceMethod string //服务名和方法名，格式Service.Method 对应结构体和结构体方法
	Seq           uint64 //请求序号，请求ID
	Error         string // 服务端有错误，放在这里返回
}

// Codec 编解码器接口
type Codec interface {
	io.Closer
	ReadHeader(*Header) error         //读取头信息
	ReadBody(interface{}) error       //读取消息体
	Write(*Header, interface{}) error //写入头信息
}

// 以下代码类型工厂模式，根据不同的编解码器类型，返回相对应的创建函数，返回编解码器实例

// 创建解码器函数类型
type NewCodecFunc func(io.ReadWriteCloser) Codec

// 编解码器类型
type Type string

// 定义编解码器标识
const (
	GobType  Type = "application/gob"
	JsonType Type = "application/json"
	Protobuf Type = "application/protobuf"
)

// 保存不同类型编解码器的创建函数
var NewCodecFuncMap map[Type]NewCodecFunc

// init 初始化不同类型编解码器的创建函数
func init() {
	NewCodecFuncMap = make(map[Type]NewCodecFunc)
	NewCodecFuncMap[GobType] = NewGobCodec
}
