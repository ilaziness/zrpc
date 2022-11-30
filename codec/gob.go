package codec

import (
	"bufio"
	"encoding/gob"
	"io"
	"log"
)

// Gob编解码

// NewGobCodec 创建Gob编解码器
func NewGobCodec(conn io.ReadWriteCloser) Codec {
	buf := bufio.NewWriter(conn)
	return &GobCodec{
		conn: conn,
		buf:  buf,
		dec:  gob.NewDecoder(conn),
		enc:  gob.NewEncoder(buf),
	}
}

// 验证GobCodec实现了Codec接口
// 确保接口被实现常用的方式的一种方式，如果实现不完整，编译期将会报错
// 将nil转成GobCodec，再赋值给Codec接口类型，如果接口没有被实现，则编译的时候会报错
var _ Codec = (*GobCodec)(nil)
var _ Codec = &GobCodec{} //和上面一行一样的目的

// Gob编解码器
type GobCodec struct {
	conn io.ReadWriteCloser //网络连接
	buf  *bufio.Writer      //数据缓冲
	dec  *gob.Decoder       //gob解码
	enc  *gob.Encoder       //gob编码
}

// 以下四个函数实现Codec接口

// ReadHeader 读取请求头，h需要是指针
func (c *GobCodec) ReadHeader(h *Header) error {
	return c.dec.Decode(h)
}

// ReadBody 读取请求体，body需要是指针
func (c *GobCodec) ReadBody(body interface{}) error {
	return c.dec.Decode(body)
}

func (c *GobCodec) Write(h *Header, body interface{}) (err error) {
	defer func() {
		_ = c.buf.Flush()
		if err != nil {
			_ = c.Close()
		}
	}()
	if err := c.enc.Encode(h); err != nil {
		log.Println("rpc codec: gob error encoding header:", err)
		return err
	}
	if err := c.enc.Encode(body); err != nil {
		log.Println("rpc codec: gob error encoding body:", err)
		return err
	}
	return nil
}

// Close 实现io.Closer接口
func (c *GobCodec) Close() error {
	return c.conn.Close()
}
