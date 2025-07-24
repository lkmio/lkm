package flv

import (
	"github.com/lkmio/avformat"
	"github.com/lkmio/avformat/collections"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/flv"
	"github.com/lkmio/flv/amf0"
	"github.com/lkmio/lkm/rtmp"
	"github.com/lkmio/lkm/stream"
)

type TransStream struct {
	stream.TCPTransStream

	Muxer             *flv.Muxer
	flvHeaderBlock    []byte // 单独保存9个字节长的flv头, 只发一次, 后续恢复推流不再发送
	flvExtraDataBlock []byte // metadata和sequence header
}

func (t *TransStream) Input(packet *avformat.AVPacket, index int) ([]*collections.ReferenceCounter[[]byte], int64, bool, error) {
	t.ClearOutStreamBuffer()

	var flvTagSize int
	var data []byte
	var videoKey bool
	var dts int64
	var pts int64
	var keyBuffer bool
	var frameType int

	duration := packet.GetDuration(1000)
	track := t.Tracks[index]
	dts = track.Dts
	pts = track.Pts
	track.Dts += duration
	track.Pts = track.Dts + packet.GetPtsDtsDelta(1000)

	if utils.AVMediaTypeAudio == packet.MediaType {
		//log.Sugar.Infof("audio packet dts: %d, pts: %d data size: %d", dts, pts, len(packet.Data))
		data = packet.Data
		flvTagSize = flv.TagHeaderSize + t.Muxer.ComputeAudioDataHeaderSize() + len(packet.Data)
	} else if utils.AVMediaTypeVideo == packet.MediaType {
		//log.Sugar.Infof("video packet dts: %d, pts: %d", dts, pts)
		data = avformat.AnnexBPacket2AVCC(packet)
		flvTagSize = flv.TagHeaderSize + t.Muxer.ComputeVideoDataHeaderSize(uint32(pts-dts)) + len(data)
		if videoKey = packet.Key; videoKey {
			frameType = flv.FrameTypeKeyFrame
		}
	}

	// 关键帧都放在切片头部，所以遇到关键帧创建新切片
	if videoKey && !t.MWBuffer.IsNewSegment() && t.MWBuffer.HasVideoDataInCurrentSegment() {
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
	} else if t.MWBuffer.ShouldFlush(dts) {
		// 切片末尾, 预留换行符
		separatorSize += 2
	}

	// 分配指定大小的内存
	bytes, ok := t.MWBuffer.TryAlloc(separatorSize+flvTagSize, dts, utils.AVMediaTypeVideo == packet.MediaType, videoKey)
	if !ok {
		segment, key := t.flushSegment()
		t.AppendOutStreamBuffer(segment)

		if !keyBuffer {
			keyBuffer = key
		}
		bytes, ok = t.MWBuffer.TryAlloc(HttpFlvBlockHeaderSize+flvTagSize, dts, utils.AVMediaTypeVideo == packet.MediaType, videoKey)
		n = HttpFlvBlockHeaderSize
		utils.Assert(ok)
	}

	// 写flv tag
	n += t.Muxer.Input(bytes[n:], packet.MediaType, len(data), dts, pts, false, frameType)
	copy(bytes[n:], data)

	// 合并写满再发
	if segment, key := t.MWBuffer.TryFlushSegment(); segment != nil {
		if !keyBuffer {
			keyBuffer = key
		}

		// 已经分配末尾换行符内存, 直接添加
		segment.ResetData(FormatSegment(segment.Get()))
		t.AppendOutStreamBuffer(segment)
	}

	return t.OutBuffer[:t.OutBufferSize], 0, keyBuffer, nil
}

func (t *TransStream) AddTrack(track *stream.Track) (int, error) {
	var index int
	var err error
	if utils.AVMediaTypeAudio == track.Stream.MediaType {
		index, err = t.Muxer.AddAudioTrack(track.Stream)
	} else if utils.AVMediaTypeVideo == track.Stream.MediaType {
		index, err = t.Muxer.AddVideoTrack(track.Stream)

		t.Muxer.MetaData().AddNumberProperty("width", float64(track.Stream.CodecParameters.Width()))
		t.Muxer.MetaData().AddNumberProperty("height", float64(track.Stream.CodecParameters.Height()))
	}

	return index, err
}

func (t *TransStream) WriteHeader() error {
	var header [4096]byte
	size := t.Muxer.WriteHeader(header[:])
	tags := header[9:size]
	copy(t.flvHeaderBlock[HttpFlvBlockHeaderSize:], header[:9])
	copy(t.flvExtraDataBlock[HttpFlvBlockHeaderSize:], tags)

	// +2 加上末尾换行符
	t.flvExtraDataBlock = t.flvExtraDataBlock[:HttpFlvBlockHeaderSize+size-9+2]
	writeSeparator(t.flvHeaderBlock)
	writeSeparator(t.flvExtraDataBlock)

	t.MWBuffer = stream.NewMergeWritingBuffer(t.HasVideo())
	return nil
}

func (t *TransStream) ReadExtraData(_ int64) ([]*collections.ReferenceCounter[[]byte], int64, error) {
	return []*collections.ReferenceCounter[[]byte]{collections.NewReferenceCounter(GetHttpFLVBlock(t.flvHeaderBlock)), collections.NewReferenceCounter(GetHttpFLVBlock(t.flvExtraDataBlock))}, 0, nil
}

func (t *TransStream) ReadKeyFrameBuffer() ([]*collections.ReferenceCounter[[]byte], int64, error) {
	t.ClearOutStreamBuffer()

	// 发送当前内存池已有的合并写切片
	t.MWBuffer.ReadSegmentsFromKeyFrameIndex(func(segment *collections.ReferenceCounter[[]byte]) {
		t.AppendOutStreamBuffer(segment)
	})

	return t.OutBuffer[:t.OutBufferSize], 0, nil
}

func (t *TransStream) Close() ([]*collections.ReferenceCounter[[]byte], int64, error) {
	t.ClearOutStreamBuffer()

	// 发送剩余的流
	if !t.MWBuffer.IsNewSegment() {
		if segment, _ := t.flushSegment(); segment != nil {
			t.AppendOutStreamBuffer(segment)
		}
	}

	return t.OutBuffer[:t.OutBufferSize], 0, nil
}

// 保存为完整的http-flv切片
func (t *TransStream) flushSegment() (*collections.ReferenceCounter[[]byte], bool) {
	// 预览末尾换行符
	t.MWBuffer.Reserve(2)
	segment, key := t.MWBuffer.FlushSegment()
	segment.ResetData(FormatSegment(segment.Get()))
	return segment, key
}

func NewHttpTransStream(metadata *amf0.Object, prevTagSize uint32) stream.TransStream {
	return &TransStream{
		Muxer:             flv.NewMuxerWithPrevTagSize(metadata, prevTagSize),
		flvHeaderBlock:    make([]byte, 31),
		flvExtraDataBlock: make([]byte, 4096),
	}
}

func TransStreamFactory(source stream.Source, _ stream.TransStreamProtocol, _ []*stream.Track, _ stream.Sink) (stream.TransStream, error) {
	var prevTagSize uint32
	var metaData *amf0.Object

	endInfo := source.GetTransStreamPublisher().GetStreamEndInfo()
	if endInfo != nil {
		prevTagSize = endInfo.FLVPrevTagSize
	}

	if stream.SourceTypeRtmp == source.GetType() {
		metaData = source.(*rtmp.Publisher).Stack.Metadata()
	}

	return NewHttpTransStream(metaData, prevTagSize), nil
}
