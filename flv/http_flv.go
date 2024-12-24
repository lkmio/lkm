package flv

import (
	"encoding/binary"
	"github.com/lkmio/avformat/libflv"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/rtmp"
	"github.com/lkmio/lkm/stream"
)

type TransStream struct {
	stream.TCPTransStream

	Muxer               libflv.Muxer
	flvHeaderBlock      []byte // 单独保存9个字节长的flv头, 只发一次, 后续恢复推流不再发送
	flvExtraDataBlock   []byte // metadata和sequence header
	flvExtraDataTagSize int    // 整个flv tag大小
}

func (t *TransStream) Input(packet utils.AVPacket) ([][]byte, int64, bool, error) {
	t.ClearOutStreamBuffer()

	var flvTagSize int
	var data []byte
	var videoKey bool
	var dts int64
	var pts int64
	var keyBuffer bool

	dts = packet.ConvertDts(1000)
	pts = packet.ConvertPts(1000)
	if utils.AVMediaTypeAudio == packet.MediaType() {
		flvTagSize = 17 + len(packet.Data())
		data = packet.Data()
	} else if utils.AVMediaTypeVideo == packet.MediaType() {
		flvTagSize = t.Muxer.ComputeVideoDataSize(uint32(pts-dts)) + libflv.TagHeaderSize + len(packet.AVCCPacketData())

		data = packet.AVCCPacketData()
		videoKey = packet.KeyFrame()
	}

	// 关键帧都放在切片头部，所以遇到关键帧创建新切片, 发送当前切片剩余流
	if videoKey && !t.MWBuffer.IsNewSegment() {
		segment, key := t.flushSegment()
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

	// 分配block
	bytes := t.MWBuffer.Allocate(separatorSize+flvTagSize, dts, videoKey)
	// 写flv tag
	n += t.Muxer.Input(bytes[n:], packet.MediaType(), len(data), dts, pts, packet.KeyFrame(), false)
	copy(bytes[n:], data)

	// 合并写满再发
	if segment, key := t.MWBuffer.PeekCompletedSegment(); len(segment) > 0 {
		keyBuffer = key
		// 已经分配末尾换行符内存, 直接添加
		t.AppendOutStreamBuffer(FormatSegment(segment))
	}

	return t.OutBuffer[:t.OutBufferSize], 0, keyBuffer, nil
}

func (t *TransStream) AddTrack(track *stream.Track) error {
	if err := t.BaseTransStream.AddTrack(track); err != nil {
		return err
	}

	if utils.AVMediaTypeAudio == track.Stream.Type() {
		t.Muxer.AddAudioTrack(track.Stream.CodecId(), 0, 0, 0)
	} else if utils.AVMediaTypeVideo == track.Stream.Type() {
		t.Muxer.AddVideoTrack(track.Stream.CodecId())

		t.Muxer.MetaData().AddNumberProperty("width", float64(track.Stream.CodecParameters().Width()))
		t.Muxer.MetaData().AddNumberProperty("height", float64(track.Stream.CodecParameters().Height()))
	}
	return nil
}

func (t *TransStream) WriteHeader() error {
	var header [4096]byte
	var extraDataSize int
	size := t.Muxer.WriteHeader(header[:])
	copy(t.flvHeaderBlock[HttpFlvBlockHeaderSize:], header[:9])
	copy(t.flvExtraDataBlock[HttpFlvBlockHeaderSize:], header[9:size])

	extraDataSize = HttpFlvBlockHeaderSize + (size - 9)
	for _, track := range t.BaseTransStream.Tracks {
		var data []byte
		if utils.AVMediaTypeAudio == track.Stream.Type() {
			data = track.Stream.Extra()
		} else if utils.AVMediaTypeVideo == track.Stream.Type() {
			data = track.Stream.CodecParameters().MP4ExtraData()
		}

		n := t.Muxer.Input(t.flvExtraDataBlock[extraDataSize:], track.Stream.Type(), len(data), 0, 0, false, true)
		extraDataSize += n
		copy(t.flvExtraDataBlock[extraDataSize:], data)
		extraDataSize += len(data)
		t.flvExtraDataTagSize = n - 15 + len(data) + 11
	}

	// 加上末尾换行符
	extraDataSize += 2
	t.flvExtraDataBlock = t.flvExtraDataBlock[:extraDataSize]
	writeSeparator(t.flvHeaderBlock)
	writeSeparator(t.flvExtraDataBlock)

	t.MWBuffer = stream.NewMergeWritingBuffer(t.ExistVideo)
	return nil
}

func (t *TransStream) ReadExtraData(_ int64) ([][]byte, int64, error) {
	return [][]byte{GetHttpFLVBlock(t.flvHeaderBlock), GetHttpFLVBlock(t.flvExtraDataBlock)}, 0, nil
}

func (t *TransStream) ReadKeyFrameBuffer() ([][]byte, int64, error) {
	t.ClearOutStreamBuffer()

	// 发送当前内存池已有的合并写切片
	t.MWBuffer.ReadSegmentsFromKeyFrameIndex(func(bytes []byte) {
		if t.OutBufferSize < 1 {
			// 修改第一个flv tag的pre tag size为sequence header tag size
			binary.BigEndian.PutUint32(bytes[HttpFlvBlockHeaderSize:], uint32(t.flvExtraDataTagSize))
		}

		// 遍历发送合并写切片
		var index int
		for ; index < len(bytes); index += 4 {
			size := binary.BigEndian.Uint32(bytes[index:])
			t.AppendOutStreamBuffer(GetHttpFLVBlock(bytes[index : index+4+int(size)]))
			index += int(size)
		}
	})

	return t.OutBuffer[:t.OutBufferSize], 0, nil
}

func (t *TransStream) Close() ([][]byte, int64, error) {
	t.ClearOutStreamBuffer()

	// 发送剩余的流
	if !t.MWBuffer.IsNewSegment() {
		if segment, _ := t.flushSegment(); len(segment) > 0 {
			t.AppendOutStreamBuffer(segment)
		}
	}

	return t.OutBuffer[:t.OutBufferSize], 0, nil
}

// 保存为完整的http-flv切片
func (t *TransStream) flushSegment() ([]byte, bool) {
	// 预览末尾换行符
	t.MWBuffer.Reserve(2)
	segment, key := t.MWBuffer.FlushSegment()
	return FormatSegment(segment), key
}

func NewHttpTransStream(metadata *libflv.AMF0Object, prevTagSize uint32) stream.TransStream {
	return &TransStream{
		Muxer:             libflv.NewMuxerWithPrevTagSize(metadata, prevTagSize),
		flvHeaderBlock:    make([]byte, 31),
		flvExtraDataBlock: make([]byte, 4096),
	}
}

func TransStreamFactory(source stream.Source, protocol stream.TransStreamProtocol, tracks []*stream.Track) (stream.TransStream, error) {
	var prevTagSize uint32
	var metaData *libflv.AMF0Object

	endInfo := source.GetStreamEndInfo()
	if endInfo != nil {
		prevTagSize = endInfo.FLVPrevTagSize
	}

	if stream.SourceTypeRtmp == source.GetType() {
		metaData = source.(*rtmp.Publisher).Stack.MetaData()
	}

	return NewHttpTransStream(metaData, prevTagSize), nil
}
