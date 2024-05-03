package flv

import (
	"encoding/binary"
	"fmt"
	"github.com/yangjiechina/avformat/libflv"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/live-server/stream"
)

const (
	// HttpFlvBlockLengthSize 在每块HttpFlv数据前面，增加指定长度的头数据, 用户描述flv数据的长度信息
	// http-flv-block  |block size[4]|skip count[2]|length\r\n|flv data\r\n
	HttpFlvBlockLengthSize = 20
)

type HttpFlvBlock struct {
	pktSize   uint32
	skipCount uint16
}

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
		headerSize: HttpFlvBlockLengthSize,
	}
}

func TransStreamFactory(source stream.ISource, protocol stream.Protocol, streams []utils.AVStream) (stream.ITransStream, error) {
	return NewHttpTransStream(), nil
}

func (t *httpTransStream) Input(packet utils.AVPacket) error {
	var flvSize int
	var data []byte
	var videoKey bool
	var dts int64
	var pts int64

	if utils.AVCodecIdAAC == packet.CodecId() {
		dts = packet.ConvertDts(1024)
		pts = packet.ConvertPts(1024)
	} else {
		dts = packet.ConvertDts(1000)
		pts = packet.ConvertPts(1000)
	}

	if utils.AVMediaTypeAudio == packet.MediaType() {
		flvSize = 17 + len(packet.Data())
		data = packet.Data()
	} else if utils.AVMediaTypeVideo == packet.MediaType() {
		flvSize = t.muxer.ComputeVideoDataSize(uint32(pts-dts)) + libflv.TagHeaderSize + len(packet.AVCCPacketData())

		data = packet.AVCCPacketData()
		videoKey = packet.KeyFrame()
	}

	if videoKey {
		head, _ := t.StreamBuffers[0].Data()
		if len(head) > t.SegmentOffset {
			//分配末尾换行符
			t.StreamBuffers[0].Allocate(2)

			head, _ = t.StreamBuffers[0].Data()
			t.writeSeparator(head[t.SegmentOffset:])
			skip := t.computeSikCount(head[t.SegmentOffset:])
			t.SendPacketWithOffset(head, t.SegmentOffset+skip)
		}

		t.SwapStreamBuffer()
	}

	var n int
	var separatorSize int
	full := t.Full(dts)
	if head, _ := t.StreamBuffers[0].Data(); t.SegmentOffset == len(head) {
		separatorSize = HttpFlvBlockLengthSize
		//10字节描述flv包长, 前2个字节描述无效字节长度
		n = HttpFlvBlockLengthSize
	}
	if full {
		separatorSize = 2
	}

	allocate := t.StreamBuffers[0].Allocate(separatorSize + flvSize)
	n += t.muxer.Input(allocate[n:], packet.MediaType(), len(data), dts, pts, packet.KeyFrame(), false)
	copy(allocate[n:], data)
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

	if utils.AVMediaTypeAudio == stream.Type() {
		t.muxer.AddAudioTrack(stream.CodecId(), 0, 0, 0)
	} else if utils.AVMediaTypeVideo == stream.Type() {
		t.muxer.AddVideoTrack(stream.CodecId())

		t.muxer.AddProperty("width", stream.CodecParameters().SPSInfo().Width())
		t.muxer.AddProperty("height", stream.CodecParameters().SPSInfo().Height())
	}
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

// 为flv数据添加长度和换行符
// @dst flv数据, 开头需要空出HttpFlvBlockLengthSize字节长度, 末尾空出2字节换行符
func (t *httpTransStream) writeSeparator(dst []byte) {
	//写block size
	binary.BigEndian.PutUint32(dst, uint32(len(dst)-4))

	//写长度字符串
	//flv长度转16进制字符串
	flvSize := len(dst) - HttpFlvBlockLengthSize - 2
	hexStr := fmt.Sprintf("%X", flvSize)
	//+2是将换行符计算在内
	n := len(hexStr) + 2
	copy(dst[HttpFlvBlockLengthSize-n:], hexStr)

	//写间隔长度
	//-6 是block size和skip count合计长度
	skipCount := HttpFlvBlockLengthSize - n - 6
	binary.BigEndian.PutUint16(dst[4:], uint16(skipCount))

	//flv长度和flv数据之间的换行符
	dst[HttpFlvBlockLengthSize-2] = 0x0D
	dst[HttpFlvBlockLengthSize-1] = 0x0A

	//末尾换行符
	dst[len(dst)-2] = 0x0D
	dst[len(dst)-1] = 0x0A
}

func (t *httpTransStream) WriteHeader() error {
	t.Init()

	t.headerSize += t.muxer.WriteHeader(t.header[HttpFlvBlockLengthSize:])

	for _, track := range t.TransStreamImpl.Tracks {
		var data []byte
		if utils.AVMediaTypeAudio == track.Type() {
			data = track.Extra()
		} else if utils.AVMediaTypeVideo == track.Type() {
			data = track.CodecParameters().DecoderConfRecord().ToMP4VC()
		}

		t.headerSize += t.muxer.Input(t.header[t.headerSize:], track.Type(), len(data), 0, 0, false, true)
		copy(t.header[t.headerSize:], data)
		t.headerSize += len(data)
	}

	//将结尾换行符计算在内
	t.headerSize += 2
	t.writeSeparator(t.header[:t.headerSize])
	return nil
}
