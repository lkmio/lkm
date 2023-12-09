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
	//当开始缓存第2组GOP的第二个合并写切片时，将上一个GOP缓存释放掉
	//使用2块内存池，保证内存连续，一次发送
	//不开启GOP缓存和只有音频包的情况下，创建使用一个MemoryPool
	//memoryPool  stream.MemoryPool
	memoryPool  [2]stream.MemoryPool
	transBuffer stream.StreamBuffer

	mwSegmentTs    int64
	lastTs         int64
	chunkSizeQueue *stream.Queue

	//发送未完整切片的Sinks
	//当AddSink时，还未缓存到一组切片，有多少先发多少. 后续切片未满之前的生成的rtmp包都将直接发送给sink.
	//只要满了一组切片后，这些sink都不单独发包, 统一发送切片.
	incompleteSinks []stream.ISink
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

	//即不开启GOP缓存又不合并发送. 直接使用AVPacket的预留头封装发送
	if !stream.AppConfig.GOPCache || t.onlyAudio {
		//首帧视频帧必须要关键帧
		return
	}

	if videoPkt && packet.KeyFrame() {
		//交替使用缓存
		tmp := t.memoryPool[0]
		t.memoryPool[0] = t.memoryPool[1]
		t.memoryPool[1] = tmp
	}

	//分配内存
	t.memoryPool[0].Mark()
	allocate := t.memoryPool[0].Allocate(12 + payloadSize + (payloadSize / t.chunkSize))

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

	var ret bool
	rtmpData := t.memoryPool[0].Fetch()[:n]
	if stream.AppConfig.GOPCache {
		//ret = t.transBuffer.AddPacket(rtmpData, packet.KeyFrame() && videoPkt, packet.Dts())
		ret = t.transBuffer.AddPacket(packet, packet.KeyFrame() && videoPkt, packet.Dts())
	}

	if !ret || stream.AppConfig.GOPCache {
		t.memoryPool[0].FreeTail()
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
		//if t.lastTs == 0 {
		//	t.transBuffer.Peek(0).(utils.AVPacket).Dts()
		//}

		endTs := t.lastTs + mergeWriteLatency
		if t.transBuffer.Peek(t.transBuffer.Size()-1).(utils.AVPacket).Dts() < endTs {
			return
		}

		head, tail := t.memoryPool[0].Data()
		sizeHead, sizeTail := t.chunkSizeQueue.Data()
		var offset int
		var size int
		var chunkSize int
		var lastTs int64
		var tailIndex int
		for i := 0; i < t.transBuffer.Size(); i++ {
			pkt := t.transBuffer.Peek(i).(utils.AVPacket)

			if i < len(sizeHead) {
				chunkSize = sizeHead[i].(int)
			} else {
				chunkSize = sizeTail[tailIndex].(int)
				tailIndex++
			}

			if pkt.Dts() <= t.lastTs && t.lastTs != 0 {
				offset += chunkSize
				continue
			}

			if pkt.Dts() > endTs {
				break
			}

			size += chunkSize
			lastTs = pkt.Dts()
		}
		t.lastTs = lastTs

		//后面再优化只发送一次
		var data1 []byte
		var data2 []byte
		if offset > len(head) {
			offset -= len(head)
			head = tail
			tail = nil
		}
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
	utils.Assert(t.headerSize > 0)

	t.TransStreamImpl.AddSink(sink)
	sink.Input(t.header[:t.headerSize])

	if !stream.AppConfig.GOPCache {
		return
	}

	//发送到最近一个合并写切片之前

	// if stream.AppConfig.GOPCache > 0 {
	// 	t.transBuffer.PeekAll(func(packet interface{}) {
	// 		sink.Input(packet.([]byte))
	// 	})
	// }
}

func (t *TransStream) onDiscardPacket(pkt interface{}) {
	t.memoryPool[0].FreeHead()
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

	if stream.AppConfig.GOPCache {
		t.transBuffer = stream.NewStreamBuffer(200)
		t.transBuffer.SetDiscardHandler(t.onDiscardPacket)

		//创建2块内存
		t.memoryPool[0] = stream.NewMemoryPoolWithRecopy(1024 * 4000)
		t.memoryPool[1] = stream.NewMemoryPoolWithRecopy(1024 * 4000)
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

func NewTransStream(chunkSize int) stream.ITransStream {
	transStream := &TransStream{chunkSize: chunkSize, TransStreamImpl: stream.TransStreamImpl{Sinks: make(map[stream.SinkId]stream.ISink, 64)}}
	transStream.chunkSizeQueue = stream.NewQueue(512)
	return transStream
}
