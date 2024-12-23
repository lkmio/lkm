package flv

import (
	"encoding/binary"
	"fmt"
	"github.com/lkmio/avformat/libflv"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/stream"
)

const (
	// HttpFlvBlockHeaderSize 在每块http-flv流的头部，预留指定大小的数据, 用于描述flv数据块的长度信息
	// http-flv是以文件流的形式传输http流, 格式如下: length\r\n|flv data\r\n
	// 我们对http-flv-block的封装: |block size[4]|skip count[2]|length\r\n|flv data\r\n
	// skip count是因为length长度不固定, 需要一个字段说明, 跳过多少字节才是http-flv数据
	HttpFlvBlockHeaderSize = 20
)

type TransStream struct {
	stream.TCPTransStream

	muxer         libflv.Muxer
	header        []byte
	headerSize    int
	headerTagSize int
}

func (t *TransStream) Input(packet utils.AVPacket) ([][]byte, int64, bool, error) {
	t.ClearOutStreamBuffer()

	var flvSize int
	var data []byte
	var videoKey bool
	var dts int64
	var pts int64
	var keyBuffer bool

	dts = packet.ConvertDts(1000)
	pts = packet.ConvertPts(1000)
	if utils.AVMediaTypeAudio == packet.MediaType() {
		flvSize = 17 + len(packet.Data())
		data = packet.Data()
	} else if utils.AVMediaTypeVideo == packet.MediaType() {
		flvSize = t.muxer.ComputeVideoDataSize(uint32(pts-dts)) + libflv.TagHeaderSize + len(packet.AVCCPacketData())

		data = packet.AVCCPacketData()
		videoKey = packet.KeyFrame()
	}

	// 关键帧都放在切片头部，所以遇到关键帧创建新切片, 发送当前切片剩余流
	if videoKey && !t.MWBuffer.IsNewSegment() {
		segment, key := t.forceFlushSegment()
		t.AppendOutStreamBuffer(segment)
		keyBuffer = key
	}

	var n int
	var separatorSize int

	// 新的合并写切片, 预留包长字节
	if t.MWBuffer.IsNewSegment() {
		separatorSize = HttpFlvBlockHeaderSize
		// 10字节描述flv包长, 前2个字节描述无效字节长度
		n = HttpFlvBlockHeaderSize
	}

	// 切片末尾, 预留换行符
	if t.MWBuffer.IsFull(dts) {
		separatorSize += 2
	}

	// 分配flv block
	bytes := t.MWBuffer.Allocate(separatorSize+flvSize, dts, videoKey)
	n += t.muxer.Input(bytes[n:], packet.MediaType(), len(data), dts, pts, packet.KeyFrame(), false)
	copy(bytes[n:], data)

	// 合并写满再发
	if segment, key := t.MWBuffer.PeekCompletedSegment(); len(segment) > 0 {
		// 已经分配末尾换行符内存
		keyBuffer = key
		t.AppendOutStreamBuffer(t.FormatSegment(segment))
	}

	return t.OutBuffer[:t.OutBufferSize], 0, keyBuffer, nil
}

func (t *TransStream) AddTrack(track *stream.Track) error {
	if err := t.BaseTransStream.AddTrack(track); err != nil {
		return err
	}

	if utils.AVMediaTypeAudio == track.Stream.Type() {
		t.muxer.AddAudioTrack(track.Stream.CodecId(), 0, 0, 0)
	} else if utils.AVMediaTypeVideo == track.Stream.Type() {
		t.muxer.AddVideoTrack(track.Stream.CodecId())

		t.muxer.MetaData().AddNumberProperty("width", float64(track.Stream.CodecParameters().Width()))
		t.muxer.MetaData().AddNumberProperty("height", float64(track.Stream.CodecParameters().Height()))
	}
	return nil
}

func (t *TransStream) WriteHeader() error {
	t.headerSize += t.muxer.WriteHeader(t.header[HttpFlvBlockHeaderSize:])

	for _, track := range t.BaseTransStream.Tracks {
		var data []byte
		if utils.AVMediaTypeAudio == track.Stream.Type() {
			data = track.Stream.Extra()
		} else if utils.AVMediaTypeVideo == track.Stream.Type() {
			data = track.Stream.CodecParameters().MP4ExtraData()
		}

		n := t.muxer.Input(t.header[t.headerSize:], track.Stream.Type(), len(data), 0, 0, false, true)
		t.headerSize += n
		copy(t.header[t.headerSize:], data)
		t.headerSize += len(data)

		t.headerTagSize = n - 15 + len(data) + 11
	}

	// 加上末尾换行符
	t.headerSize += 2
	t.writeSeparator(t.header[:t.headerSize])

	t.MWBuffer = stream.NewMergeWritingBuffer(t.ExistVideo)
	return nil
}

func (t *TransStream) ReadExtraData(_ int64) ([][]byte, int64, error) {
	utils.Assert(t.headerSize > 0)
	// 发送sequence header
	return [][]byte{t.GetHttpFLVBlock(t.header[:t.headerSize])}, 0, nil
}

func (t *TransStream) ReadKeyFrameBuffer() ([][]byte, int64, error) {
	t.ClearOutStreamBuffer()

	// 发送当前内存池已有的合并写切片
	t.MWBuffer.ReadSegmentsFromKeyFrameIndex(func(bytes []byte) {
		if t.OutBufferSize < 1 {
			// 修改第一个flv tag的pre tag size
			binary.BigEndian.PutUint32(bytes[HttpFlvBlockHeaderSize:], uint32(t.headerTagSize))
		}

		// 遍历发送合并写切片
		var index int
		for ; index < len(bytes); index += 4 {
			size := binary.BigEndian.Uint32(bytes[index:])
			t.AppendOutStreamBuffer(t.GetHttpFLVBlock(bytes[index : index+4+int(size)]))
			index += int(size)
		}
	})

	return t.OutBuffer[:t.OutBufferSize], 0, nil
}

func (t *TransStream) Close() ([][]byte, int64, error) {
	t.ClearOutStreamBuffer()

	// 发送剩余的流
	if !t.MWBuffer.IsNewSegment() {
		if segment, _ := t.forceFlushSegment(); len(segment) > 0 {
			t.AppendOutStreamBuffer(segment)
		}
	}

	return t.OutBuffer[:t.OutBufferSize], 0, nil
}

// 保存为完整的http-flv切片
func (t *TransStream) forceFlushSegment() ([]byte, bool) {
	// 预览末尾换行符
	t.MWBuffer.Reserve(2)
	segment, key := t.MWBuffer.FlushSegment()
	return t.FormatSegment(segment), key
}

// GetHttpFLVBlock 跳过头部的无效数据，返回http-flv块
func (t *TransStream) GetHttpFLVBlock(data []byte) []byte {
	return data[t.computeSkipBytesSize(data):]
}

// FormatSegment 为切片添加包长和换行符
func (t *TransStream) FormatSegment(segment []byte) []byte {
	t.writeSeparator(segment)
	return t.GetHttpFLVBlock(segment)
}

func (t *TransStream) computeSkipBytesSize(data []byte) int {
	return int(6 + binary.BigEndian.Uint16(data[4:]))
}

// 为http-flv数据块添加长度和换行符
// @dst http-flv数据块, 头部需要空出HttpFlvBlockLengthSize字节长度, 末尾空出2字节换行符
func (t *TransStream) writeSeparator(dst []byte) {
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

func NewHttpTransStream() stream.TransStream {
	return &TransStream{
		muxer:      libflv.NewMuxer(nil),
		header:     make([]byte, 1024),
		headerSize: HttpFlvBlockHeaderSize,
	}
}

func TransStreamFactory(source stream.Source, protocol stream.TransStreamProtocol, tracks []*stream.Track) (stream.TransStream, error) {
	return NewHttpTransStream(), nil
}
