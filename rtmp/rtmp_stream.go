package rtmp

import (
	"github.com/yangjiechina/avformat/libflv"
	"github.com/yangjiechina/avformat/librtmp"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/live-server/stream"
)

type TransStream struct {
	stream.TransStreamImpl
	chunkSize int
	//sequence header
	header     []byte
	headerSize int
	muxer      *libflv.Muxer

	//只存在音频流
	onlyAudio  bool
	audioChunk librtmp.Chunk
	videoChunk librtmp.Chunk

	//只需要缓存一组GOP+第2组GOP的第一个合并写切片
	//当缓存到第2组GOP的第二个合并写切片时，将上一个GOP缓存释放掉
	//使用2块内存池，分别缓存2个GOP，保证内存连续，一次发送
	//不开启GOP缓存和只有音频包的情况下，创建使用一个MemoryPool
	memoryPool [2]stream.MemoryPool

	//当前合并写切片的缓存时长
	segmentDuration int
	//当前合并写切片位于memoryPool的开始偏移量
	segmentOffset int
	//前一个包的时间戳
	prePacketTS int64

	firstVideoPacket bool

	//发送未完整切片的Sinks
	//当AddSink时，还未缓存到一组切片，有多少先发多少. 后续切片未满之前的生成的rtmp包都将直接发送给sink.
	//只要满了一组切片后，这些sink都不单独发包, 统一发送切片.
	incompleteSinks []stream.ISink
}

func NewTransStream(chunkSize int) stream.ITransStream {
	transStream := &TransStream{chunkSize: chunkSize, TransStreamImpl: stream.TransStreamImpl{Sinks: make(map[stream.SinkId]stream.ISink, 64)}}
	return transStream
}

func (t *TransStream) Input(packet utils.AVPacket) {
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
		//首帧必须要视频关键帧
		if !t.firstVideoPacket {
			if !packet.KeyFrame() {
				return
			}

			t.firstVideoPacket = true
		}

		videoPkt = true
		videoKey = packet.KeyFrame()
		data = packet.AVCCPacketData()
		length = len(data)
		chunk = &t.videoChunk
		payloadSize += 5 + length
	}

	//即不开启GOP缓存又不合并发送. 直接使用AVPacket的预留头封装发送
	if !stream.AppConfig.GOPCache || t.onlyAudio {
		//首帧视频帧必须要关键帧
		return
	}

	if videoKey {
		tmp := t.memoryPool[0]
		head, _ := tmp.Data()
		if len(head) > t.segmentOffset {
			for _, sink := range t.Sinks {
				sink.Input(head[t.segmentOffset:])
			}
		}

		t.memoryPool[0].Clear()
		//交替使用缓存
		t.memoryPool[0] = t.memoryPool[1]
		t.memoryPool[1] = tmp

		t.segmentDuration = 0
		t.segmentOffset = 0
	}

	//分配内存
	t.memoryPool[0].Mark()
	allocate := t.memoryPool[0].Allocate(12 + payloadSize + (payloadSize / t.chunkSize))

	//写chunk头
	chunk.Length = payloadSize
	chunk.Timestamp = uint32(packet.Pts())
	n := chunk.ToBytes(allocate)
	utils.Assert(n == 12)

	//写flv
	ct := packet.Pts() - packet.Dts()
	if videoPkt {
		n += t.muxer.WriteVideoData(allocate[12:], uint32(ct), packet.KeyFrame(), false)
	} else {
		n += t.muxer.WriteAudioData(allocate[12:], false)
	}

	first := true
	for length > 0 {
		var min int
		if first {
			min = utils.MinInt(length, t.chunkSize-5)
			first = false
		} else {
			min = utils.MinInt(length, t.chunkSize)
		}

		copy(allocate[n:], data[:min])
		n += min

		length -= min
		data = data[min:]

		//写一个ChunkType3用作分割
		if length > 0 {
			if videoPkt {
				allocate[n] = (0x3 << 6) | byte(librtmp.ChunkStreamIdVideo)
			} else {
				allocate[n] = (0x3 << 6) | byte(librtmp.ChunkStreamIdAudio)
			}
			n++
		}
	}

	rtmpData := t.memoryPool[0].Fetch()[:n]
	t.segmentDuration += int(packet.Pts() - t.prePacketTS)
	t.prePacketTS = packet.Pts()

	//给不完整切片的Sink补齐包
	if len(t.incompleteSinks) > 0 {
		for _, sink := range t.incompleteSinks {
			sink.Input(rtmpData)
		}

		if t.segmentDuration >= stream.AppConfig.MergeWriteLatency {
			head, tail := t.memoryPool[0].Data()
			utils.Assert(len(tail) == 0)

			t.segmentOffset = len(head)
			t.segmentDuration = 0
			t.incompleteSinks = nil
		}

		return
	}

	if t.segmentDuration < stream.AppConfig.MergeWriteLatency {
		return
	}

	head, tail := t.memoryPool[0].Data()
	utils.Assert(len(tail) == 0)
	for _, sink := range t.Sinks {
		sink.Input(head[t.segmentOffset:])
	}

	t.segmentOffset = len(head)
	t.segmentDuration = 0

	//当缓存到第2组GOP的第二个合并写切片时，将上一个GOP缓存释放掉
	if t.segmentOffset > len(head) && t.memoryPool[1] != nil && !t.memoryPool[1].Empty() {
		t.memoryPool[1].Clear()
	}
}

func (t *TransStream) AddSink(sink stream.ISink) {
	utils.Assert(t.headerSize > 0)

	t.TransStreamImpl.AddSink(sink)
	//发送sequence header
	sink.Input(t.header[:t.headerSize])

	if !stream.AppConfig.GOPCache || t.onlyAudio {
		return
	}

	//发送当前内存池已有的合并写切片
	if t.segmentOffset > 0 {
		data, tail := t.memoryPool[0].Data()
		utils.Assert(len(data) > 0)
		utils.Assert(len(tail) == 0)
		sink.Input(data[:t.segmentOffset])
		return
	}

	//发送上一组GOP
	if t.memoryPool[1] != nil && !t.memoryPool[1].Empty() {
		data, tail := t.memoryPool[0].Data()
		utils.Assert(len(data) > 0)
		utils.Assert(len(tail) == 0)
		sink.Input(data)
		return
	}

	//不足一个合并写切片, 有多少发多少
	data, tail := t.memoryPool[0].Data()
	utils.Assert(len(tail) == 0)
	if len(data) > 0 {
		sink.Input(data)
		t.incompleteSinks = append(t.incompleteSinks, sink)
	}
}

func (t *TransStream) WriteHeader() error {
	utils.Assert(t.Tracks != nil)
	utils.Assert(!t.TransStreamImpl.Completed)

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
	t.muxer = libflv.NewMuxer(audioCodecId, videoCodecId, 0, 0, 0)

	if stream.AppConfig.GOPCache {
		//创建2块内存
		t.memoryPool[0] = stream.NewMemoryPoolWithDirect(1024*4000, true)
		t.memoryPool[1] = stream.NewMemoryPoolWithDirect(1024*4000, true)
	} else {

	}

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
