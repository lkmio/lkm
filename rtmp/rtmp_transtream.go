package rtmp

import (
	"github.com/yangjiechina/avformat/libflv"
	"github.com/yangjiechina/avformat/librtmp"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/live-server/stream"
)

type TransStream struct {
	stream.TransStreamImpl
	chunkSize  int
	header     []byte //音视频头chunk
	headerSize int
	muxer      *libflv.Muxer

	audioChunk librtmp.Chunk
	videoChunk librtmp.Chunk

	memoryPool     stream.MemoryPool
	transBuffer    stream.StreamBuffer
	lastTs         int64
	chunkSizeQueue *stream.Queue
}

func (t *TransStream) Input(packet utils.AVPacket) {
	utils.Assert(t.TransStreamImpl.Completed)
	var data []byte
	var chunk *librtmp.Chunk
	var videoPkt bool
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
		data = packet.AVCCPacketData()
		length = len(data)
		chunk = &t.videoChunk
		payloadSize += 5 + length
	}

	//payloadSize += payloadSize / t.chunkSize
	//分配内存
	t.memoryPool.Mark()
	allocate := t.memoryPool.Allocate(12 + payloadSize + (payloadSize / t.chunkSize))

	//写chunk头
	chunk.Length = payloadSize
	chunk.Timestamp = uint32(packet.Dts())
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
	var min int
	for length > 0 {
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

	rtmpData := t.memoryPool.Fetch()[:n]
	ret := true
	if stream.AppConfig.GOPCache > 0 {
		//ret = t.transBuffer.AddPacket(rtmpData, packet.KeyFrame() && videoPkt, packet.Dts())
		ret = t.transBuffer.AddPacket(packet, packet.KeyFrame() && videoPkt, packet.Dts())
	}

	if !ret || stream.AppConfig.GOPCache < 1 {
		t.memoryPool.FreeTail()
	}

	if ret {
		//发送给sink
		mergeWriteLatency := int64(350)

		if mergeWriteLatency == 0 {
			for _, sink := range t.Sinks {
				sink.Input(rtmpData)
			}

			return
		}

		t.chunkSizeQueue.Push(len(rtmpData))
		if t.lastTs == 0 {
			t.transBuffer.Peek(0).(utils.AVPacket).Dts()
		}

		if mergeWriteLatency > t.transBuffer.Peek(t.transBuffer.Size()-1).(utils.AVPacket).Dts()-t.lastTs {
			return
		}

		head, tail := t.memoryPool.Data()
		queueHead, queueTail := t.chunkSizeQueue.All()
		var offset int
		var size int
		endTs := t.lastTs + mergeWriteLatency
		for i := 0; i < t.transBuffer.Size(); i++ {
			pkt := t.transBuffer.Peek(i).(utils.AVPacket)
			if pkt.Dts() < t.lastTs {
				if i < len(queueHead) {
					offset += queueHead[i].(int)
				} else {
					offset += queueTail[i+1%len(queueTail)].(int)
				}
				continue
			}
			if pkt.Dts() > endTs {
				break
			}

			size += len(pkt.Data())
			t.lastTs = pkt.Dts()
		}

		var data1 []byte
		var data2 []byte
		if offset+size > len(head) {
			data1 = head[offset:]
			size -= len(head[offset:])
			data2 = tail[:size]
		} else {
			data1 = head[offset : offset+size]
		}

		for _, sink := range t.Sinks {
			if data1 != nil {
				sink.Input(data1)
			}

			if data2 != nil {
				sink.Input(data2)
			}
		}
	}
}

func (t *TransStream) AddSink(sink stream.ISink) {
	t.TransStreamImpl.AddSink(sink)

	utils.Assert(t.headerSize > 0)
	sink.Input(t.header[:t.headerSize])

	// if stream.AppConfig.GOPCache > 0 {
	// 	t.transBuffer.PeekAll(func(packet interface{}) {
	// 		sink.Input(packet.([]byte))
	// 	})
	// }
}

func (t *TransStream) onDiscardPacket(pkt interface{}) {
	t.memoryPool.FreeHead()
	t.chunkSizeQueue.Pop()
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
	t.memoryPool = stream.NewMemoryPool(1024 * 1000 * (stream.AppConfig.GOPCache + 1))
	if stream.AppConfig.GOPCache > 0 {
		t.transBuffer = stream.NewStreamBuffer(int64(stream.AppConfig.GOPCache * 200))
		t.transBuffer.SetDiscardHandler(t.onDiscardPacket)
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

func NewTransStream(chunkSize int) stream.ITransStream {
	transStream := &TransStream{chunkSize: chunkSize, TransStreamImpl: stream.TransStreamImpl{Sinks: make(map[stream.SinkId]stream.ISink, 64)}}
	transStream.chunkSizeQueue = stream.NewQueue(512)
	return transStream
}
