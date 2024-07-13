package flv

import (
	"encoding/binary"
	"fmt"
	"github.com/lkmio/avformat/libflv"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/stream"
)

const (
	// HttpFlvBlockHeaderSize 在每块HttpFlv块头部，预留指定大小的数据, 用于描述flv数据块的长度信息
	//实际发送流 http-flv: |length\r\n|flv data\r\n
	//方便封装 http-flv-block: |block size[4]|skip count[2]|length\r\n|flv data\r\n
	//skip count是因为flv-length不固定, 需要一个字段说明, 跳过多少字节才是http-flv数据
	HttpFlvBlockHeaderSize = 20
)

type httpTransStream struct {
	stream.TCPTransStream

	muxer         libflv.Muxer
	mwBuffer      stream.MergeWritingBuffer
	header        []byte
	headerSize    int
	headerTagSize int
}

func (t *httpTransStream) Input(packet utils.AVPacket) error {
	var flvSize int
	var data []byte
	var videoKey bool
	var dts int64
	var pts int64

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

	//关键帧都放在切片头部，所以需要创建新切片, 发送当前切片剩余流
	if videoKey && !t.mwBuffer.IsNewSegment() {
		t.forceFlushSegment()
	}

	var n int
	var separatorSize int

	//新的合并写切片, 预留包长字节
	if t.mwBuffer.IsNewSegment() {
		separatorSize = HttpFlvBlockHeaderSize
		//10字节描述flv包长, 前2个字节描述无效字节长度
		n = HttpFlvBlockHeaderSize
	}

	//切片末尾, 预留换行符
	if t.mwBuffer.IsFull(dts) {
		separatorSize += 2
	}

	//分配flv block
	bytes := t.mwBuffer.Allocate(separatorSize+flvSize, dts, videoKey)
	n += t.muxer.Input(bytes[n:], packet.MediaType(), len(data), dts, pts, packet.KeyFrame(), false)
	copy(bytes[n:], data)

	//合并写满再发
	if segment := t.mwBuffer.PeekCompletedSegment(); len(segment) > 0 {
		t.sendUnpackedSegment(segment)
	}
	return nil
}

func (t *httpTransStream) AddTrack(stream utils.AVStream) error {
	if err := t.BaseTransStream.AddTrack(stream); err != nil {
		return err
	}

	if utils.AVMediaTypeAudio == stream.Type() {
		t.muxer.AddAudioTrack(stream.CodecId(), 0, 0, 0)
	} else if utils.AVMediaTypeVideo == stream.Type() {
		t.muxer.AddVideoTrack(stream.CodecId())

		t.muxer.AddProperty("width", stream.CodecParameters().Width())
		t.muxer.AddProperty("height", stream.CodecParameters().Height())
	}
	return nil
}

func (t *httpTransStream) AddSink(sink stream.Sink) error {
	utils.Assert(t.headerSize > 0)

	t.TCPTransStream.AddSink(sink)
	//发送sequence header
	t.sendSegment(sink, t.header[:t.headerSize])

	//发送当前内存池已有的合并写切片
	first := true
	t.mwBuffer.ReadSegmentsFromKeyFrameIndex(func(bytes []byte) {
		if first {
			//修改第一个flv tag的pre tag size
			binary.BigEndian.PutUint32(bytes[20:], uint32(t.headerTagSize))
			first = false
		}

		//遍历发送合并写切片
		var index int
		for ; index < len(bytes); index += 4 {
			size := binary.BigEndian.Uint32(bytes[index:])
			t.sendSegment(sink, bytes[index:index+4+int(size)])
			index += int(size)
		}
	})

	return nil
}

func (t *httpTransStream) WriteHeader() error {
	t.headerSize += t.muxer.WriteHeader(t.header[HttpFlvBlockHeaderSize:])

	for _, track := range t.BaseTransStream.Tracks {
		var data []byte
		if utils.AVMediaTypeAudio == track.Type() {
			data = track.Extra()
		} else if utils.AVMediaTypeVideo == track.Type() {
			data = track.CodecParameters().MP4ExtraData()
		}

		n := t.muxer.Input(t.header[t.headerSize:], track.Type(), len(data), 0, 0, false, true)
		t.headerSize += n
		copy(t.header[t.headerSize:], data)
		t.headerSize += len(data)

		t.headerTagSize = n - 15 + len(data) + 11
	}

	//加上末尾换行符
	t.headerSize += 2
	t.writeSeparator(t.header[:t.headerSize])

	t.mwBuffer = stream.NewMergeWritingBuffer(t.ExistVideo)
	return nil
}

func (t *httpTransStream) Close() error {
	//发送剩余的流
	if !t.mwBuffer.IsNewSegment() {
		t.forceFlushSegment()
	}
	return nil
}

func (t *httpTransStream) forceFlushSegment() {
	t.mwBuffer.Reserve(2)
	segment := t.mwBuffer.FlushSegment()
	t.sendUnpackedSegment(segment)
}

// 为单个sink发送flv切片, 切片已经添加分隔符
func (t *httpTransStream) sendSegment(sink stream.Sink, data []byte) error {
	return sink.Input(data[t.computeSkipCount(data):])
}

// 发送还未添加包长和换行符的切片
func (t *httpTransStream) sendUnpackedSegment(segment []byte) {
	t.writeSeparator(segment)
	skip := t.computeSkipCount(segment)
	t.SendPacket(segment[skip:])
}

func (t *httpTransStream) computeSkipCount(data []byte) int {
	return int(6 + binary.BigEndian.Uint16(data[4:]))
}

// 为http-flv数据块添加长度和换行符
// @dst http-flv数据块, 头部需要空出HttpFlvBlockLengthSize字节长度, 末尾空出2字节换行符
func (t *httpTransStream) writeSeparator(dst []byte) {
	//http-flv: length\r\n|flv data\r\n
	//http-flv-block: |block size[4]|skip count[2]|length\r\n|flv data\r\n

	//写block size
	binary.BigEndian.PutUint32(dst, uint32(len(dst)-4))

	//写flv实际长度字符串, 16进制表达
	flvSize := len(dst) - HttpFlvBlockHeaderSize - 2
	hexStr := fmt.Sprintf("%X", flvSize)
	//+2是跳过length后的换行符
	n := len(hexStr) + 2
	copy(dst[HttpFlvBlockHeaderSize-n:], hexStr)

	//写跳过字节数量
	//-6是block size和skip count字段合计长度
	skipCount := HttpFlvBlockHeaderSize - n - 6
	binary.BigEndian.PutUint16(dst[4:], uint16(skipCount))

	//flv length字段和flv数据之间的换行符
	dst[HttpFlvBlockHeaderSize-2] = 0x0D
	dst[HttpFlvBlockHeaderSize-1] = 0x0A

	//末尾换行符
	dst[len(dst)-2] = 0x0D
	dst[len(dst)-1] = 0x0A
}

func NewHttpTransStream() stream.TransStream {
	return &httpTransStream{
		muxer:      libflv.NewMuxer(),
		header:     make([]byte, 1024),
		headerSize: HttpFlvBlockHeaderSize,
	}
}

func TransStreamFactory(source stream.Source, protocol stream.Protocol, streams []utils.AVStream) (stream.TransStream, error) {
	return NewHttpTransStream(), nil
}
