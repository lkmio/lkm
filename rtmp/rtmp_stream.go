package rtmp

import (
	"github.com/lkmio/avformat/libflv"
	"github.com/lkmio/avformat/librtmp"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/stream"
)

type transStream struct {
	stream.TCPTransStream

	chunkSize int

	header     []byte //sequence header
	headerSize int

	muxer      libflv.Muxer
	audioChunk librtmp.Chunk
	videoChunk librtmp.Chunk
	metaData   *libflv.AMF0Object // 推流方携带的元数据
}

func (t *transStream) Input(packet utils.AVPacket) ([][]byte, int64, bool, error) {
	t.ClearOutStreamBuffer()

	var data []byte
	var chunk *librtmp.Chunk
	var videoPkt bool
	var videoKey bool
	// rtmp chunk消息体的数据大小
	var payloadSize int
	// 先向rtmp buffer写的flv头,再按照chunk size分割,所以第一个chunk要跳过flv头大小
	var chunkPayloadOffset int
	var dts int64
	var pts int64
	var keyBuffer bool

	dts = packet.ConvertDts(1000)
	pts = packet.ConvertPts(1000)
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

	// 遇到视频关键帧, 发送剩余的流, 创建新切片
	if videoKey {
		if segment, key := t.MWBuffer.FlushSegment(); len(segment) > 0 {
			keyBuffer = key
			t.AppendOutStreamBuffer(segment)
		}
	}

	// type为0的header大小
	chunkHeaderSize := 12
	// type为3的chunk数量
	numChunks := (payloadSize - 1) / t.chunkSize
	rtmpMsgSize := chunkHeaderSize + payloadSize + numChunks
	// 如果时间戳超过3字节, 每个chunk都需要多4字节的扩展时间戳
	if dts >= 0xFFFFFF && dts <= 0xFFFFFFFF {
		rtmpMsgSize += (1 + numChunks) * 4
	}

	// 分配指定大小的内存
	allocate := t.MWBuffer.Allocate(rtmpMsgSize, dts, videoKey)

	// 写第一个type为0的chunk header
	chunk.Length = payloadSize
	chunk.Timestamp = uint32(dts)
	n := chunk.MarshalHeader(allocate)

	// 封装成flv
	if videoPkt {
		n += t.muxer.WriteVideoData(allocate[n:], uint32(ct), packet.KeyFrame(), false)
	} else {
		n += t.muxer.WriteAudioData(allocate[n:], false)
	}

	// 将flv data写入chunk body
	n += chunk.WriteBody(allocate[n:], data, t.chunkSize, chunkPayloadOffset)
	utils.Assert(len(allocate) == n)

	// 合并写满了再发
	if segment, key := t.MWBuffer.PeekCompletedSegment(); len(segment) > 0 {
		keyBuffer = key
		t.AppendOutStreamBuffer(segment)
	}

	return t.OutBuffer[:t.OutBufferSize], 0, keyBuffer, nil
}

func (t *transStream) ReadExtraData(_ int64) ([][]byte, int64, error) {
	utils.Assert(t.headerSize > 0)

	// 发送sequence header
	return [][]byte{t.header[:t.headerSize]}, 0, nil
}

func (t *transStream) ReadKeyFrameBuffer() ([][]byte, int64, error) {
	t.ClearOutStreamBuffer()

	// 发送当前内存池已有的合并写切片
	t.MWBuffer.ReadSegmentsFromKeyFrameIndex(func(bytes []byte) {
		t.AppendOutStreamBuffer(bytes)
	})

	return t.OutBuffer[:t.OutBufferSize], 0, nil
}

func (t *transStream) WriteHeader() error {
	utils.Assert(t.Tracks != nil)
	utils.Assert(!t.BaseTransStream.Completed)

	var audioStream utils.AVStream
	var videoStream utils.AVStream
	var audioCodecId utils.AVCodecID
	var videoCodecId utils.AVCodecID

	for _, track := range t.Tracks {
		if utils.AVMediaTypeAudio == track.Stream.Type() {
			audioStream = track.Stream
			audioCodecId = audioStream.CodecId()
			t.audioChunk = librtmp.NewAudioChunk()
		} else if utils.AVMediaTypeVideo == track.Stream.Type() {
			videoStream = track.Stream
			videoCodecId = videoStream.CodecId()
			t.videoChunk = librtmp.NewVideoChunk()
		}
	}

	utils.Assert(audioStream != nil || videoStream != nil)

	// 初始化
	t.BaseTransStream.Completed = true
	t.muxer = libflv.NewMuxer(t.metaData)
	if utils.AVCodecIdNONE != audioCodecId {
		t.muxer.AddAudioTrack(audioCodecId, 0, 0, 0)
	}

	if utils.AVCodecIdNONE != videoCodecId {
		t.muxer.AddVideoTrack(videoCodecId)
	}

	var n int
	t.header = make([]byte, 4096)

	// 生成推流的数据头(chunk+sequence header)
	if audioStream != nil {
		n += t.muxer.WriteAudioData(t.header[12:], true)
		extra := audioStream.Extra()
		copy(t.header[n+12:], extra)
		n += len(extra)

		t.audioChunk.Length = n
		t.audioChunk.MarshalHeader(t.header)
		n += 12
	}

	if videoStream != nil {
		tmp := n
		n += t.muxer.WriteVideoData(t.header[n+12:], 0, false, true)
		extra := videoStream.CodecParameters().MP4ExtraData()
		copy(t.header[n+12:], extra)
		n += len(extra)

		t.videoChunk.Length = 5 + len(extra)
		t.videoChunk.MarshalHeader(t.header[tmp:])
		n += 12
	}

	// 创建元数据chunk
	var body [1024]byte
	amf0 := libflv.AMF0{}
	amf0.AddString("onMetaData")
	amf0.Add(t.muxer.MetaData())
	length, _ := amf0.Marshal(body[:])

	metaData := librtmp.Chunk{
		Type:           librtmp.ChunkType0,
		ChunkStreamID_: 5,
		Timestamp:      0,
		TypeID:         librtmp.MessageTypeIDDataAMF0,
		StreamID:       1,
		Body:           body[:length],
		Length:         length,
	}

	var tmp [1600]byte
	size := metaData.Marshal(tmp[:], librtmp.ChunkSize)
	// metadata 放在sequence之前
	copy(t.header[size:], t.header[:n])
	copy(t.header, tmp[:][:size])

	n += size
	t.headerSize = n
	t.MWBuffer = stream.NewMergeWritingBuffer(t.ExistVideo)
	return nil
}

func (t *transStream) Close() ([][]byte, int64, error) {
	t.ClearOutStreamBuffer()

	// 发送剩余的流
	if segment, _ := t.MWBuffer.FlushSegment(); len(segment) > 0 {
		t.AppendOutStreamBuffer(segment)
	}

	return t.OutBuffer[:t.OutBufferSize], 0, nil
}

func NewTransStream(chunkSize int, metaData *libflv.AMF0Object) stream.TransStream {
	return &transStream{chunkSize: chunkSize, metaData: metaData}
}

func TransStreamFactory(source stream.Source, protocol stream.TransStreamProtocol, tracks []*stream.Track) (stream.TransStream, error) {
	// 获取推流的元数据
	var metaData *libflv.AMF0Object
	if stream.SourceTypeRtmp == source.GetType() {
		metaData = source.(*Publisher).Stack.MetaData()
	}
	return NewTransStream(librtmp.ChunkSize, metaData), nil
}
