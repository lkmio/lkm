package flv

import (
	"encoding/binary"
	"github.com/lkmio/avformat"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/flv"
	"github.com/lkmio/flv/amf0"
	"github.com/lkmio/lkm/rtmp"
	"github.com/lkmio/lkm/stream"
)

type TransStream struct {
	stream.TCPTransStream

	Muxer                  *flv.Muxer
	flvHeaderBlock         []byte // 单独保存9个字节长的flv头, 只发一次, 后续恢复推流不再发送
	flvExtraDataBlock      []byte // metadata和sequence header
	flvExtraDataPreTagSize uint32
}

func (t *TransStream) Input(packet *avformat.AVPacket) ([][]byte, int64, bool, error) {
	t.ClearOutStreamBuffer()

	var flvTagSize int
	var data []byte
	var videoKey bool
	var dts int64
	var pts int64
	var keyBuffer bool
	var frameType int

	dts = packet.ConvertDts(1000)
	pts = packet.ConvertPts(1000)
	if utils.AVMediaTypeAudio == packet.MediaType {
		data = packet.Data
		flvTagSize = flv.TagHeaderSize + t.Muxer.ComputeAudioDataHeaderSize() + len(packet.Data)
	} else if utils.AVMediaTypeVideo == packet.MediaType {
		data = avformat.AnnexBPacket2AVCC(packet)
		flvTagSize = flv.TagHeaderSize + t.Muxer.ComputeVideoDataHeaderSize(uint32(pts-dts)) + len(data)
		if videoKey = packet.Key; videoKey {
			frameType = flv.FrameTypeKeyFrame
		}
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
	n += t.Muxer.Input(bytes[n:], packet.MediaType, len(data), dts, pts, false, frameType)
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

	if utils.AVMediaTypeAudio == track.Stream.MediaType {
		t.Muxer.AddAudioTrack(track.Stream)
	} else if utils.AVMediaTypeVideo == track.Stream.MediaType {
		t.Muxer.AddVideoTrack(track.Stream)

		t.Muxer.MetaData().AddNumberProperty("width", float64(track.Stream.CodecParameters.Width()))
		t.Muxer.MetaData().AddNumberProperty("height", float64(track.Stream.CodecParameters.Height()))
	}
	return nil
}

func (t *TransStream) WriteHeader() error {
	var header [4096]byte
	size := t.Muxer.WriteHeader(header[:])
	tags := header[9:size]
	copy(t.flvHeaderBlock[HttpFlvBlockHeaderSize:], header[:9])
	copy(t.flvExtraDataBlock[HttpFlvBlockHeaderSize:], tags)

	t.flvExtraDataPreTagSize = t.Muxer.PrevTagSize()

	// +2 加上末尾换行符
	t.flvExtraDataBlock = t.flvExtraDataBlock[:HttpFlvBlockHeaderSize+size-9+2]
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
		// 修改第一个flv tag的pre tag size为sequence header tag size
		if t.OutBufferSize < 1 {
			binary.BigEndian.PutUint32(bytes[HttpFlvBlockHeaderSize:], t.flvExtraDataPreTagSize)
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

func NewHttpTransStream(metadata *amf0.Object, prevTagSize uint32) stream.TransStream {
	return &TransStream{
		Muxer:             flv.NewMuxerWithPrevTagSize(metadata, prevTagSize),
		flvHeaderBlock:    make([]byte, 31),
		flvExtraDataBlock: make([]byte, 4096),
	}
}

func TransStreamFactory(source stream.Source, protocol stream.TransStreamProtocol, tracks []*stream.Track) (stream.TransStream, error) {
	var prevTagSize uint32
	var metaData *amf0.Object

	endInfo := source.GetStreamEndInfo()
	if endInfo != nil {
		prevTagSize = endInfo.FLVPrevTagSize
	}

	if stream.SourceTypeRtmp == source.GetType() {
		metaData = source.(*rtmp.Publisher).Stack.Metadata()
	}

	return NewHttpTransStream(metaData, prevTagSize), nil
}
