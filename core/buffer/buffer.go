// Package buffer 实现零拷贝连续字节缓冲区
// 核心设计借鉴 catai 项目：通过 []*[]byte 指针切片引用预编码的 JSON 片段，
// 通过闭包实现动态 io.Reader，避免字符串拼接和内存复制。
package buffer

import (
	"bytes"
	"encoding/json"
	"io"
)

// Buffer 连续缓冲区，内部维护 []*[]byte 切片
// 每个元素指向预编码的字节切片，支持零拷贝拼接
type Buffer []*[]byte

// bufferCall 闭包函数类型，实现 io.Reader 接口
type bufferCall func(p []byte) (n int, err error)

// Read 实现 io.Reader 接口
func (call bufferCall) Read(p []byte) (n int, err error) { return call(p) }

// New 创建新的 Buffer 实例
func New() Buffer { return make(Buffer, 0) }

// AddBytes 添加字节切片指针（零拷贝，不分配新内存）
// 注意：传入的 data 切片内容在 Buffer.Get() 读取期间不应被修改
func (b *Buffer) AddBytes(data []byte) {
	ptr := new([]byte)
	*ptr = make([]byte, len(data))
	copy(*ptr, data)
	*b = append(*b, ptr)
}

// AddString 将字符串转换为字节切片后添加
func (b *Buffer) AddString(s string) {
	ptr := new([]byte)
	*ptr = []byte(s)
	*b = append(*b, ptr)
}

// AddPtr 直接添加外部字节切片指针（最灵活，零拷贝）
func (b *Buffer) AddPtr(ptr *[]byte) {
	*b = append(*b, ptr)
}

// ReadJSON 将数据序列化为 JSON 后更新缓冲区第 i 个位置的内容
// 使用原地序列化，避免额外内存分配
func (b Buffer) ReadJSON(i int, v any) error {
	if i < len(b) {
		buf := bytes.NewBuffer((*b[i])[:0])
		err := json.NewEncoder(buf).Encode(v)
		if err != nil {
			return err
		}
		*b[i] = buf.Bytes()
	}
	return nil
}

// Len 返回缓冲区中的片段数量
func (b Buffer) Len() int { return len(b) }

// Reset 清空缓冲区但保留底层数组
func (b *Buffer) Reset() {
	*b = (*b)[:0]
}

// Get 返回一个 io.Reader，可连续读取所有缓冲区内容
// 返回的 Reader 反映了调用 Get 时刻的缓冲区快照
// 每次调用 Get 都会创建一个新的独立 Reader
// 注意：返回的 Reader 不是并发安全的，应在单个 goroutine 中使用
func (b *Buffer) Get() io.Reader {
	// 捕获当前缓冲区状态
	buf := *b
	var i, y int
	return (bufferCall)(func(p []byte) (int, error) {
		if len(p) == 0 || len(buf) == i {
			return 0, io.EOF
		}
		x := 0
		for {
			ptr := buf[i]
			z := copy(p[x:], (*ptr)[y:])
			x, y = x+z, y+z
			if x >= len(p) {
				break // 读取缓冲区已满
			}
			if y >= len(*ptr) {
				i++   // 移动到下一个片段
				y = 0 // 重置读取位置
			}
			if i >= len(buf) {
				break // 所有片段读取完成
			}
		}
		return x, nil
	})
}

// Bytes 将所有片段合并为单个连续字节切片
// 调用方获取所有权，后续 buffer 变更不影响该切片
func (b *Buffer) Bytes() []byte {
	buf := *b
	totalLen := 0
	for _, ptr := range buf {
		totalLen += len(*ptr)
	}
	merged := make([]byte, totalLen)
	pos := 0
	for _, ptr := range buf {
		pos += copy(merged[pos:], *ptr)
	}
	return merged
}

// GetReader 返回一个并发安全的 io.ReadSeeker（通过预先合并所有片段）
// 适用于需要多次读取或并发读取的场景
// 注意：会分配新内存，失去零拷贝优势
func (b *Buffer) GetReader() io.Reader {
	buf := *b
	totalLen := 0
	for _, ptr := range buf {
		totalLen += len(*ptr)
	}
	merged := make([]byte, totalLen)
	pos := 0
	for _, ptr := range buf {
		pos += copy(merged[pos:], *ptr)
	}
	return bytes.NewReader(merged)
}
