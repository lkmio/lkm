package rtmp

import (
	"github.com/yangjiechina/avformat/libflv"
	"github.com/yangjiechina/avformat/librtmp"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/live-server/stream"
)

type TransStream struct {
	stream.CacheTransStream
	chunkSize int
	//sequence header
	header     []byte
	headerSize int
	muxer      libflv.Muxer

	audioChunk librtmp.Chunk
	videoChunk librtmp.Chunk
}

func NewTransStream(chunkSize int) stream.ITransStream {
	transStream := &TransStream{chunkSize: chunkSize}
	return transStream
}

func (t *TransStream) Input(packet utils.AVPacket) error {
	utils.Assert(t.TransStreamImpl.Completed)

	var data []byte
	var chunk *librtmp.Chunk
	var videoPkt bool
	var videoKey bool
	var length int
	//rtmp chunk消息体的数据大小
	var payloadSize int

	if utils.AVMediaTypeAudio == packet.MediaType() {
		data = packet.Data()
		length = len(data)
		chunk = &t.audioChunk
		payloadSize += 2 + length

	} else if utils.AVMediaTypeVideo == packet.MediaType() {
		videoPkt = true
		videoKey = packet.KeyFrame()
		data = packet.AVCCPacketData()
		length = len(data)
		chunk = &t.videoChunk
		payloadSize += 5 + length
	}

	if videoKey {
		tmp := t.StreamBuffers[0]
		head, _ := tmp.Data()
		t.SendPacket(head[t.SegmentOffset:])
		t.SwapStreamBuffer()
	}

	//分配内存
	t.StreamBuffers[0].Mark()
	allocate := t.StreamBuffers[0].Allocate(12 + payloadSize + (payloadSize / t.chunkSize))

	//写chunk头
	var ts uint32
	if packet.CodecId() == utils.AVCodecIdAAC {
		ts = uint32(packet.ConvertPts(libflv.AACFrameSize))
	} else {
		ts = uint32(packet.ConvertPts(1000))
	}

	chunk.Length = payloadSize
	chunk.Timestamp = ts
	n := chunk.ToBytes(allocate)
	utils.Assert(n == 12)

	//写flv
	ct := packet.Pts() - packet.Dts()
	if videoPkt {
		n += t.muxer.WriteVideoData(allocate[12:], uint32(ct), packet.KeyFrame(), false)
		n += chunk.WriteData(allocate[n:], data, t.chunkSize)
	} else {
		n += t.muxer.WriteAudioData(allocate[12:], false)
		n += chunk.WriteData(allocate[n:], data, t.chunkSize)
	}

	_ = t.StreamBuffers[0].Fetch()[:n]
	if t.Full(packet.Pts()) {
		return nil
	}

	head, _ := t.StreamBuffers[0].Data()
	t.SendPacketWithOffset(head[:], t.SegmentOffset)
	return nil
}

func (t *TransStream) AddSink(sink stream.ISink) error {
	utils.Assert(t.headerSize > 0)

	t.TransStreamImpl.AddSink(sink)
	//发送sequence header
	sink.Input(t.header[:t.headerSize])

	//发送当前内存池已有的合并写切片
	if t.SegmentOffset > 0 {
		data, _ := t.StreamBuffers[0].Data()
		utils.Assert(len(data) > 0)
		sink.Input(data[:t.SegmentOffset])
		return nil
	}

	//发送上一组GOP
	if t.StreamBuffers[1] != nil && !t.StreamBuffers[1].Empty() {
		data, _ := t.StreamBuffers[0].Data()
		utils.Assert(len(data) > 0)
		sink.Input(data)
		return nil
	}

	return nil
}

func (t *TransStream) WriteHeader() error {
	utils.Assert(t.Tracks != nil)
	utils.Assert(!t.TransStreamImpl.Completed)

	t.Init()

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
	t.TransStreamImpl.Completed = true
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
		extra := videoStream.Extra()
		copy(t.header[n+12:], extra)
		n += len(extra)

		t.videoChunk.Length = 5 + len(extra)
		t.videoChunk.ToBytes(t.header[tmp:])
		n += 12
	}

	t.headerSize = n
	return nil
}
