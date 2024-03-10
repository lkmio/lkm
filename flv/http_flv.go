package flv

import (
	"encoding/binary"
	"fmt"
	"github.com/yangjiechina/avformat/libflv"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/live-server/stream"
)

const (
	// HttpFlvBlockLengthSize 响应flv数据时，需要添加flv块长度, 缓存时预留该大小字节
	HttpFlvBlockLengthSize = 20
)

var separator []byte

func init() {
	separator = make([]byte, 2)
	separator[0] = 0x0D
	separator[1] = 0x0A
}

type httpTransStream struct {
	stream.CacheTransStream
	muxer      libflv.Muxer
	header     []byte
	headerSize int
}

func NewHttpTransStream() stream.ITransStream {
	return &httpTransStream{
		muxer:      libflv.NewMuxer(),
		header:     make([]byte, 1024),
		headerSize: HttpFlvBlockLengthSize + 9,
	}
}

func (t *httpTransStream) Input(packet utils.AVPacket) error {
	var flvSize int
	var data []byte
	var videoKey bool

	if utils.AVMediaTypeAudio == packet.MediaType() {
		flvSize = 17 + len(packet.Data())
		data = packet.Data()
	} else if utils.AVMediaTypeVideo == packet.MediaType() {
		flvSize = 20 + len(packet.AVCCPacketData())
		data = packet.AVCCPacketData()
		videoKey = packet.KeyFrame()
	}

	if videoKey {
		head, _ := t.StreamBuffers[0].Data()
		if len(head) > t.SegmentOffset {
			t.StreamBuffers[0].Mark()
			t.StreamBuffers[0].Allocate(2)
			t.StreamBuffers[0].Fetch()

			head, _ = t.StreamBuffers[0].Data()
			t.writeSeparator(head[t.SegmentOffset:])
			skip := t.computeSikCount(head[t.SegmentOffset:])
			t.SendPacketWithOffset(head, t.SegmentOffset+skip)
		}

		t.SwapStreamBuffer()
	}

	var n int
	var separatorSize int
	full := t.Full(packet.Pts())
	if head, _ := t.StreamBuffers[0].Data(); t.SegmentOffset == len(head) {
		separatorSize = HttpFlvBlockLengthSize
		//10字节描述flv包长, 前2个字节描述无效字节长度
		n = HttpFlvBlockLengthSize
	}
	if full {
		separatorSize = 2
	}

	t.StreamBuffers[0].Mark()
	allocate := t.StreamBuffers[0].Allocate(separatorSize + flvSize)
	n += t.muxer.Input(allocate[n:], packet.MediaType(), len(data), packet.Dts(), packet.Pts(), packet.KeyFrame(), false)
	copy(allocate[n:], data)
	_ = t.StreamBuffers[0].Fetch()

	if !full {
		return nil
	}

	head, _ := t.StreamBuffers[0].Data()
	//添加长度和换行符
	//每一个合并写切片开始和预留长度所需的字节数
	//合并写切片末尾加上换行符
	//长度是16进制字符串
	t.writeSeparator(head[t.SegmentOffset:])

	skip := t.computeSikCount(head[t.SegmentOffset:])
	t.SendPacketWithOffset(head, t.SegmentOffset+skip)
	return nil
}

func (t *httpTransStream) AddTrack(stream utils.AVStream) error {
	if err := t.TransStreamImpl.AddTrack(stream); err != nil {
		return err
	}

	var data []byte
	if utils.AVMediaTypeAudio == stream.Type() {
		t.muxer.AddAudioTrack(stream.CodecId(), 0, 0, 0)
		data = stream.Extra()
	} else if utils.AVMediaTypeVideo == stream.Type() {
		t.muxer.AddVideoTrack(stream.CodecId())
		data, _ = stream.M4VCExtraData()
	}

	t.headerSize += t.muxer.Input(t.header[t.headerSize:], stream.Type(), len(data), 0, 0, false, true)
	copy(t.header[t.headerSize:], data)
	t.headerSize += len(data)
	return nil
}

func (t *httpTransStream) sendBuffer(sink stream.ISink, data []byte) error {
	return sink.Input(data[t.computeSikCount(data):])
}

func (t *httpTransStream) computeSikCount(data []byte) int {
	return int(6 + binary.BigEndian.Uint16(data[4:]))
}

func (t *httpTransStream) AddSink(sink stream.ISink) error {
	utils.Assert(t.headerSize > 0)

	t.TransStreamImpl.AddSink(sink)
	//发送sequence header
	t.sendBuffer(sink, t.header[:t.headerSize])

	send := func(sink stream.ISink, data []byte) {
		var index int
		for ; index < len(data); index += 4 {
			size := binary.BigEndian.Uint32(data[index:])
			t.sendBuffer(sink, data[index:index+4+int(size)])
			index += int(size)
		}
	}

	//发送当前内存池已有的合并写切片
	if t.SegmentOffset > 0 {
		data, _ := t.StreamBuffers[0].Data()
		utils.Assert(len(data) > 0)
		send(sink, data[:t.SegmentOffset])
		return nil
	}

	//发送上一组GOP
	if t.StreamBuffers[1] != nil && !t.StreamBuffers[1].Empty() {
		data, _ := t.StreamBuffers[1].Data()
		utils.Assert(len(data) > 0)
		send(sink, data)
		return nil
	}

	return nil
}

func (t *httpTransStream) writeSeparator(dst []byte) {

	dst[HttpFlvBlockLengthSize-2] = 0x0D
	dst[HttpFlvBlockLengthSize-1] = 0x0A

	flvSize := len(dst) - HttpFlvBlockLengthSize - 2
	hexStr := fmt.Sprintf("%X", flvSize)
	//长度+换行符
	n := len(hexStr) + 2
	binary.BigEndian.PutUint16(dst[4:], uint16(HttpFlvBlockLengthSize-n-6))
	copy(dst[HttpFlvBlockLengthSize-n:], hexStr)

	dst[HttpFlvBlockLengthSize+flvSize] = 0x0D
	dst[HttpFlvBlockLengthSize+flvSize+1] = 0x0A

	binary.BigEndian.PutUint32(dst, uint32(len(dst)-4))
}

func (t *httpTransStream) WriteHeader() error {
	t.Init()

	_ = t.muxer.WriteHeader(t.header[HttpFlvBlockLengthSize:])
	t.headerSize += 2
	t.writeSeparator(t.header[:t.headerSize])
	return nil
}
