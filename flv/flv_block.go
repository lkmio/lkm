package flv

import (
	"encoding/binary"
	"fmt"
)

const (
	// HttpFlvBlockHeaderSize 在每块http-flv流的头部，预留指定大小的数据, 用于描述flv数据块的长度信息
	// http-flv是以文件流的形式传输http流, 格式如下: length\r\n|flv data\r\n
	// 我们对http-flv-block的封装: |block size[4]|skip count[2]|length\r\n|flv data\r\n
	// skip count是因为length长度不固定, 需要一个字段说明, 跳过多少字节才是http-flv数据
	HttpFlvBlockHeaderSize = 20
)

// GetHttpFLVBlock 跳过头部的无效数据，返回http-flv块
func GetHttpFLVBlock(data []byte) []byte {
	return data[computeSkipBytesSize(data):]
}

// GetFLVTag 从http flv块中提取返回flv tag
func GetFLVTag(block []byte) []byte {
	length := len(block)
	var offset int

	for i := 2; i < length; i++ {
		if block[i-2] == 0x0D && block[i-1] == 0x0A {
			offset = i
			break
		}
	}

	return block[offset : length-2]
}

// 计算头部的无效数据, 返回http-flv的其实位置
func computeSkipBytesSize(data []byte) int {
	return int(6 + binary.BigEndian.Uint16(data[4:]))
}

// FormatSegment 为切片添加包长和换行符
func FormatSegment(segment []byte) []byte {
	writeSeparator(segment)
	return GetHttpFLVBlock(segment)
}

// 为http-flv数据块添加长度和换行符
// @dst http-flv数据块, 头部需要空出HttpFlvBlockLengthSize字节长度, 末尾空出2字节换行符
func writeSeparator(dst []byte) {
	// http-flv: length\r\n|flv data\r\n
	// http-flv-block: |block size[4]|skip count[2]|length\r\n|flv data\r\n

	// 写block size
	binary.BigEndian.PutUint32(dst, uint32(len(dst)-4))

	// 写flv实际长度字符串, 16进制表达
	flvSize := len(dst) - HttpFlvBlockHeaderSize - 2
	hexStr := fmt.Sprintf("%X", flvSize)
	// +2是跳过length后的换行符
	n := len(hexStr) + 2
	copy(dst[HttpFlvBlockHeaderSize-n:], hexStr)

	// 写跳过字节数量
	// -6是block size和skip count字段合计长度
	skipCount := HttpFlvBlockHeaderSize - n - 6
	binary.BigEndian.PutUint16(dst[4:], uint16(skipCount))

	// flv length字段和flv数据之间的换行符
	dst[HttpFlvBlockHeaderSize-2] = 0x0D
	dst[HttpFlvBlockHeaderSize-1] = 0x0A

	// 末尾换行符
	dst[len(dst)-2] = 0x0D
	dst[len(dst)-1] = 0x0A
}
