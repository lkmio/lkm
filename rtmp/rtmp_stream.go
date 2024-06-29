package rtmp

import (
	"github.com/yangjiechina/avformat/libflv"
	"github.com/yangjiechina/avformat/librtmp"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/lkm/stream"
)

type transStream struct {
	stream.BaseTransStream

	chunkSize int

	header     []byte //sequence header
	headerSize int

	muxer      libflv.Muxer
	audioChunk librtmp.Chunk
	videoChunk librtmp.Chunk

	//合并写内存泄露问题: 推流结束后, mwBuffer的data一直释放不掉, 只有拉流全部断开之后, 才会释放该内存.
	//起初怀疑是代码层哪儿有问题, 但是测试发现如果将合并写切片再拷贝一次发送 给sink, 推流结束后，mwBuffer的data内存块释放没问题, 只有拷贝的内存块未释放. 所以排除了代码层造成内存泄露的可能性.
	//看来是conn在write后还会持有data. 查阅代码发现, 的确如此. 向fd发送数据前buffer会引用data, 但是后续没有赋值为nil, 取消引用. https://github.com/golang/go/blob/d38f1d13fa413436d38d86fe86d6a146be44bb84/src/internal/poll/fd_windows.go#L694
	mwBuffer stream.MergeWritingBuffer
}

func (t *transStream) Input(packet utils.AVPacket) error {
	utils.Assert(t.BaseTransStream.Completed)

	var data []byte
	var chunk *librtmp.Chunk
	var videoPkt bool
	var videoKey bool
	//rtmp chunk消息体的数据大小
	var payloadSize int
	//先向rtmp buffer写的flv头,再按照chunk size分割,所以第一个chunk要跳过flv头大小
	var chunkPayloadOffset int
	var dts int64
	var pts int64
	chunkHeaderSize := 12

	dts = packet.ConvertDts(1000)
	pts = packet.ConvertPts(1000)
	if dts >= 0xFFFFFF {
		chunkHeaderSize += 4
	}

	ct := pts - dts

	if utils.AVMediaTypeAudio == packet.MediaType() {
		data = packet.Data()
		chunk = &t.audioChunk
		chunkPayloadOffset = 2
		payloadSize += chunkPayloadOffset + len(data)
	} else if utils.AVMediaTypeVideo == packet.MediaType() {
		videoPkt = true
		videoKey = packet.KeyFrame()
		data = packet.AVCCPacketData()
		chunk = &t.videoChunk
		chunkPayloadOffset = t.muxer.ComputeVideoDataSize(uint32(ct))
		payloadSize += chunkPayloadOffset + len(data)
	}

	//遇到视频关键帧, 发送剩余的流
	if videoKey {
		segment := t.mwBuffer.PopSegment()
		if len(segment) > 0 {
			t.SendPacket(segment)
		}
	}

	//分配内存
	allocate := t.mwBuffer.Allocate(chunkHeaderSize + payloadSize + ((payloadSize - 1) / t.chunkSize))

	//写rtmp chunk header
	chunk.Length = payloadSize
	chunk.Timestamp = uint32(dts)
	n := chunk.ToBytes(allocate)

	//写flv
	if videoPkt {
		n += t.muxer.WriteVideoData(allocate[chunkHeaderSize:], uint32(ct), packet.KeyFrame(), false)
	} else {
		n += t.muxer.WriteAudioData(allocate[chunkHeaderSize:], false)
	}

	n += chunk.WriteData(allocate[n:], data, t.chunkSize, chunkPayloadOffset)

	segment := t.mwBuffer.PeekCompletedSegment(dts)
	if len(segment) > 0 {
		t.SendPacket(segment)
	}
	return nil
}

func (t *transStream) AddSink(sink stream.Sink) error {
	utils.Assert(t.headerSize > 0)

	t.BaseTransStream.AddSink(sink)
	//发送sequence header
	sink.Input(t.header[:t.headerSize])

	//发送当前内存池已有的合并写切片
	segmentList := t.mwBuffer.SegmentList()
	if len(segmentList) > 0 {
		sink.Input(segmentList)
		return nil
	}

	return nil
}

func (t *transStream) WriteHeader() error {
	utils.Assert(t.Tracks != nil)
	utils.Assert(!t.BaseTransStream.Completed)

	var audioStream utils.AVStream
	var videoStream utils.AVStream
	var audioCodecId utils.AVCodecID
	var videoCodecId utils.AVCodecID

	for _, track := range t.Tracks {
		if utils.AVMediaTypeAudio == track.Type() {
			audioStream = track
			audioCodecId = audioStream.CodecId()
			t.audioChunk = librtmp.NewAudioChunk()
		} else if utils.AVMediaTypeVideo == track.Type() {
			videoStream = track
			videoCodecId = videoStream.CodecId()
			t.videoChunk = librtmp.NewVideoChunk()
		}
	}

	utils.Assert(audioStream != nil || videoStream != nil)

	//初始化
	t.BaseTransStream.Completed = true
	t.header = make([]byte, 1024)
	t.muxer = libflv.NewMuxer()
	if utils.AVCodecIdNONE != audioCodecId {
		t.muxer.AddAudioTrack(audioCodecId, 0, 0, 0)
	}

	if utils.AVCodecIdNONE != videoCodecId {
		t.muxer.AddVideoTrack(videoCodecId)
	}

	//统一生成rtmp拉流需要的数据头(chunk+sequence header)
	var n int
	if audioStream != nil {
		n += t.muxer.WriteAudioData(t.header[12:], true)
		extra := audioStream.Extra()
		copy(t.header[n+12:], extra)
		n += len(extra)

		t.audioChunk.Length = n
		t.audioChunk.ToBytes(t.header)
		n += 12
	}

	if videoStream != nil {
		tmp := n
		n += t.muxer.WriteVideoData(t.header[n+12:], 0, false, true)
		extra := videoStream.CodecParameters().MP4ExtraData()
		copy(t.header[n+12:], extra)
		n += len(extra)

		t.videoChunk.Length = 5 + len(extra)
		t.videoChunk.ToBytes(t.header[tmp:])
		n += 12
	}

	t.headerSize = n

	t.mwBuffer = stream.NewMergeWritingBuffer(t.ExistVideo)
	return nil
}

func (t *transStream) Close() error {
	//发送剩余的流
	segment := t.mwBuffer.PopSegment()
	if len(segment) > 0 {
		t.SendPacket(segment)
	}
	return nil
}

func NewTransStream(chunkSize int) stream.TransStream {
	return &transStream{chunkSize: chunkSize}
}

func TransStreamFactory(source stream.Source, protocol stream.Protocol, streams []utils.AVStream) (stream.TransStream, error) {
	return NewTransStream(librtmp.ChunkSize), nil
}
