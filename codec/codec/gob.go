package codec

import (
	"bufio"
	"encoding/gob"
	"io"
	"log"
)

type GobCodec struct {
	conn io.ReadWriteCloser
	buf  *bufio.Writer
	dec  *gob.Decoder
	enc  *gob.Encoder
}

var _ Codec = (*GobCodec)(nil) // 为了检查 GobCodec 实现了 Codec 接口

func NewGobCodec(conn io.ReadWriteCloser) Codec {
	buf := bufio.NewWriter(conn)
	return &GobCodec{
		conn: conn,
		buf:  buf,
		dec:  gob.NewDecoder(conn),
		enc:  gob.NewEncoder(buf),
	}
}

func (c GobCodec) ReadHeader(h *Header) error {
	return c.dec.Decode(h)
}

// ReadBody 从连接中读取数据到 body 中
func (c GobCodec) ReadBody(body any) error {
	return c.dec.Decode(body)
}

// Write 将请求头 h 和 body 写入连接中
func (c GobCodec) Write(h *Header, body any) (err error) {
	defer func() {
		_ = c.buf.Flush()
		if err != nil {
			_ = c.Close()
		}
	}()
	if err = c.enc.Encode(h); err != nil {
		log.Println("rpc codec: gob error encoding header:", err)
		return
	}
	if err = c.enc.Encode(body); err != nil {
		log.Println("rpc codec: gob error encoding body:", err)
		return
	}
	return nil
}

func (c GobCodec) Close() error {
	return c.conn.Close()
}
