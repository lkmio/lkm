package stream

import (
	"fmt"
	"github.com/lkmio/avformat"
	"github.com/lkmio/avformat/collections"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/flv"
	"github.com/lkmio/lkm/log"
	"github.com/lkmio/lkm/transcode"
	"github.com/lkmio/transport"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

type StreamEventType int

const (
	StreamEventTypeTrack StreamEventType = iota + 1
	StreamEventTypeTrackCompleted
	StreamEventTypePacket
	StreamEventTypeRawPacket
)

type StreamEvent struct {
	Type StreamEventType
	Data interface{}
}

type TransStreamPublisher interface {
	Post(event *StreamEvent)

	start()

	close()

	Sinks() []Sink

	GetTransStreams() map[TransStreamID]TransStream

	GetForwardTransStream() TransStream

	GetStreamEndInfo() *StreamEndInfo

	// SinkCount 返回拉流计数
	SinkCount() int

	// LastStreamEndTime 返回最近结束拉流时间戳
	LastStreamEndTime() time.Time

	// TranscodeTracks 返回所有的转码track
	TranscodeTracks() []*Track

	// AddSink 添加Sink, 在此之前请确保Sink已经握手、授权通过. 如果Source还未WriteHeader，先将Sink添加到等待队列.
	// 匹配拉流期望的编码器, 创建TransStream或向已经存在TransStream添加Sink
	AddSink(sink Sink)

	// RemoveSink 同步删除Sink
	RemoveSink(sink Sink)

	RemoveSinkWithID(id SinkID)

	FindSink(id SinkID) Sink

	ExecuteSyncEvent(cb func())

	SetSourceID(id string)

	// StartRecord 开启录制
	// 如果AppConfig已经开启了全局录制, 则无需手动开启, 返回false
	StartRecord() bool

	// StopRecord 停止录制
	// 如果AppConfig已经开启了全局录制, 返回error
	StopRecord() error

	RecordStartTime() time.Time

	GetRecordStreamPlayUrl() string
}

type transStreamPublisher struct {
	source            string
	streamEvents      *NonBlockingChannel[*StreamEvent]
	mainContextEvents chan func()
	earlyEvents       collections.LinkedList[func()] // 早于启动前的事件, 等待启动后执行

	sinkCount int       // 拉流计数
	gopBuffer GOPBuffer // GOP缓存, 音频和视频混合使用, 以视频关键帧为界, 缓存第二个视频关键帧时, 释放前一组gop

	recordSink         Sink                                // 每个Source的录制流
	recordFilePath     string                              // 录制流文件路径
	recordStartTime    time.Time                           // 开始录制时间
	hasManualRecording bool                                // 是否开启手动录像
	hlsStream          TransStream                         // HLS传输流
	originTracks       TrackManager                        // 推流的原始track
	transcodeTracks    map[utils.AVCodecID]*TranscodeTrack // 转码Track

	transStreams       map[TransStreamID]TransStream     // 所有输出流
	forwardTransStream TransStream                       // 转发流
	sinks              map[SinkID]Sink                   // 所有拉流Sink
	transStreamSinks   map[TransStreamID]map[SinkID]Sink // 输出流对应的Sink

	hasVideo              bool        // 是否存在视频
	completed             atomic.Bool // 推流track是否解析完毕
	closed                atomic.Bool
	streamEndInfo         *StreamEndInfo             // 上次结束推流的信息
	lastStreamEndTime     time.Time                  // 最近结束拉流的时间
	bitstreamFilterBuffer *collections.RBBlockBuffer // annexb和avcc转换的缓冲区
	mute                  sync.Mutex
	started               bool
}

func (t *transStreamPublisher) Post(event *StreamEvent) {
	t.streamEvents.Post(event)
}

func (t *transStreamPublisher) run() {
	defer func() {
		// 清空管道
		for event := t.streamEvents.Pop(); event != nil; event = t.streamEvents.Pop() {
			if StreamEventTypePacket == event.Type {
				event.Data.(*collections.ReferenceCounter[*avformat.AVPacket]).Release()
			}
		}
	}()

	for {
		select {
		case event := <-t.streamEvents.Channel:
			switch event.Type {
			case StreamEventTypeTrack:
				// 添加track
				t.OnNewTrack(event.Data.(*Track))
			case StreamEventTypeTrackCompleted:
				t.WriteHeader()
				// track完成
			case StreamEventTypePacket:
				// 发送数据包
				t.OnPacket(event.Data.(*collections.ReferenceCounter[*avformat.AVPacket]))
			case StreamEventTypeRawPacket:
				// 发送原始数据包, 目前仅用于国标级联转发
				if t.forwardTransStream != nil && t.forwardTransStream.GetProtocol() == TransStreamGBCascaded {
					packets := event.Data.([][]byte)
					for _, data := range packets {
						t.DispatchPacketToStream(t.forwardTransStream, &avformat.AVPacket{Data: data[2:]})
						UDPReceiveBufferPool.Put(data[:cap(data)])
					}
				}
			}
		case event := <-t.mainContextEvents:
			event()
			if t.closed.Load() {
				return
			}
		}
	}
}

func (t *transStreamPublisher) start() {
	t.mute.Lock()
	defer t.mute.Unlock()

	t.streamEvents = NewNonBlockingChannel[*StreamEvent](256)
	t.mainContextEvents = make(chan func(), 256)

	t.transStreams = make(map[TransStreamID]TransStream, 10)
	t.sinks = make(map[SinkID]Sink, 128)
	t.transStreamSinks = make(map[TransStreamID]map[SinkID]Sink, len(transStreamFactories)+1)
	t.transcodeTracks = make(map[utils.AVCodecID]*TranscodeTrack, 4)

	go t.run()
	t.started = true

	// 放置先于启动的事件到主管道
	for t.earlyEvents.Size() > 0 {
		t.mainContextEvents <- t.earlyEvents.Remove(0)
	}
}

func (t *transStreamPublisher) PostEvent(cb func()) {
	if t.started {
		t.mainContextEvents <- cb
		return
	}

	// 早于启动前的事件, 添加到等待队列
	t.mute.Lock()
	defer t.mute.Unlock()
	if !t.started {
		t.earlyEvents.Add(cb)
	}
}

func (t *transStreamPublisher) ExecuteSyncEvent(cb func()) {
	group := sync.WaitGroup{}
	group.Add(1)

	t.PostEvent(func() {
		cb()
		group.Done()
	})

	group.Wait()
}

func (t *transStreamPublisher) createRecordSink() bool {
	sink, path, err := CreateRecordStream(t.source)
	if err != nil {
		log.Sugar.Errorf("创建录制sink失败 source: %s err: %s", t.source, err.Error())
		return false
	}

	t.recordSink = sink
	t.recordFilePath = path
	t.recordStartTime = time.Now()
	return true
}

func (t *transStreamPublisher) CreateDefaultOutStreams() {
	if t.transStreams == nil {
		t.transStreams = make(map[TransStreamID]TransStream, 10)
	}

	// 创建录制流
	if AppConfig.Record.Enable || t.hasManualRecording {
		t.createRecordSink()
	}

	// 创建HLS输出流
	if AppConfig.Hls.Enable {
		streams := t.originTracks.All()
		utils.Assert(len(streams) > 0)

		hlsStream, err := t.CreateTransStream(TransStreamHls, streams, nil)
		if err != nil {
			log.Sugar.Errorf("创建HLS输出流失败 source: %s err: %s", t.source, err.Error())
			return
		}

		t.DispatchGOPBuffer(hlsStream)
		t.hlsStream = hlsStream
	}
}

// 转码GOPBuffer中的音频
func (t *transStreamPublisher) transcodeGOPBuffer(track *TranscodeTrack) {
	if t.gopBuffer != nil {
		t.gopBuffer.PeekAll(func(packet *collections.ReferenceCounter[*avformat.AVPacket]) {
			if utils.AVMediaTypeAudio != packet.Get().MediaType {
				return
			}
			track.Input(packet.Get())
		})
	}
}

func (t *transStreamPublisher) CreateTransStream(protocol TransStreamProtocol, tracks []*Track, sink Sink) (TransStream, error) {
	// 匹配和创建适合TransStream流协议的track
	var finalTracks []*Track
	for _, track := range tracks {
		// 传输流支持的编码器列表
		supportedCodecs, ok := SupportedCodes[protocol]
		if !ok {
			panic(fmt.Sprintf("unknown protocol %s", protocol.String()))
		}

		// 是否支持该编码器
		_, ok = supportedCodecs[track.Stream.CodecID]

		// 如果PCM采样率不符合FLV的标准, 也开启转码
		if ok && utils.AVCodecIdPCMS16LE == track.Stream.CodecID && (TransStreamRtmp == protocol || TransStreamFlv == protocol) {
			for _, sampleRate := range flv.SupportedSampleRates {
				ok = sampleRate == track.Stream.SampleRate
				if ok {
					break
				}
			}

			if !ok {
				log.Sugar.Warnf("FLV不支持的PCM采样率 source: %s stream: %s sampleRate: %d", t.source, protocol.String(), track.Stream.SampleRate)
			}
		}

		if !ok {
			log.Sugar.Warnf("不支持的编码器 source: %s stream: %s codec: %s", t.source, protocol.String(), track.Stream.CodecID)
			// 如果没有开启音频转码或非音频流，跳过
			if utils.AVMediaTypeAudio != track.Stream.MediaType || transcode.CreateAudioTranscoder == nil {
				continue
			}

			var transcodeTrack *TranscodeTrack
			// 优先从已经转码的track列表中查找支持的track
			for _, old := range t.transcodeTracks {
				if _, ok = supportedCodecs[old.transcoder.GetEncoderID()]; ok {
					transcodeTrack = old
					break
				}
			}

			if transcodeTrack == nil {
				// 创建音频转码器
				var codecs []utils.AVCodecID
				for codec := range supportedCodecs {
					codecs = append(codecs, codec)
				}

				transcoder, stream, err := transcode.CreateAudioTranscoder(track.Stream, codecs)
				if err != nil {
					log.Sugar.Errorf("创建音频转码器失败 source: %s stream: %s codec: %s err: %s", t.source, protocol.String(), track.Stream.CodecID, err.Error())
					continue
				}

				log.Sugar.Infof("创建音频转码器成功 source: %s stream: %s src: %s dst: %s", t.source, protocol.String(), track.Stream.CodecID, transcoder.GetEncoderID())

				stream.Index = len(t.originTracks.tracks) + len(t.transcodeTracks)
				newTrack := &Track{Stream: stream}

				transcodeTrack = NewTranscodeTrack(newTrack, transcoder)
				t.transcodeTracks[transcoder.GetEncoderID()] = transcodeTrack

				// 转码GOP中的推流音频
				t.transcodeGOPBuffer(transcodeTrack)
			} else {
				log.Sugar.Infof("使用已经存在的音频转码track source: %s stream: %s src: %s dst: %s", t.source, protocol.String(), track.Stream.CodecID, transcodeTrack.transcoder.GetEncoderID())
			}

			track = transcodeTrack.track
		}

		// 创建新的track
		newTrack := &Track{
			Stream: track.Stream,
		}
		finalTracks = append(finalTracks, newTrack)
	}

	if len(finalTracks) < 1 {
		return nil, fmt.Errorf("not found track")
	}

	id := GenerateTransStreamID(protocol, finalTracks...)
	// 如果已经存在该id的输出流, 则直接返回
	oldTransStream := t.transStreams[id]
	if oldTransStream != nil {
		return oldTransStream, nil
	}

	log.Sugar.Infof("创建%s-stream source: %s", protocol.String(), t.source)

	source := SourceManager.Find(t.source)
	utils.Assert(source != nil)
	transStream, err := CreateTransStream(source, protocol, tracks, sink)
	if err != nil {
		return nil, err
	}

	for _, track := range finalTracks {
		index, err := transStream.AddTrack(track)
		if err != nil {
			log.Sugar.Errorf("添加track失败 err: %s source: %s stream: %s, codec: %s ", err.Error(), t.source, protocol, track.Stream.CodecID)
			continue
		}

		// stream index->muxer track index
		transStream.SetMuxerTrack(index, track)
	}

	if transStream.TrackSize() == 0 {
		return nil, fmt.Errorf("not found track")
	}

	id = GenerateTransStreamID(protocol, transStream.GetTracks()...)
	transStream.SetID(id)
	transStream.SetProtocol(protocol)

	// 使用上次推流结束的时间戳
	if t.streamEndInfo != nil {
		oldTimestamps, ok := t.streamEndInfo.Timestamps[id]
		if ok {
			for _, track := range transStream.GetTracks() {
				track.Dts = oldTimestamps[track.Stream.CodecID][0]
				track.Pts = oldTimestamps[track.Stream.CodecID][1]

				log.Sugar.Debugf("使用上次结束推流的时间戳 source: %s stream: %s track: %s dts: %d pts: %d", t.source, protocol, track.Stream.CodecID, track.Dts, track.Pts)
			}
		}
	}

	// 尝试清空等待释放的合并写缓冲区
	ReleasePendingBuffers(t.source, id)

	t.transStreams[id] = transStream
	// 创建输出流对应的拉流队列
	t.transStreamSinks[id] = make(map[SinkID]Sink, 128)
	_ = transStream.WriteHeader()

	// 设置转发流
	if TransStreamGBCascaded == transStream.GetProtocol() {
		t.forwardTransStream = transStream
	} else if AppConfig.GOPCache && t.hasVideo {
		// 新建传输流,发送GOP缓存
		t.DispatchGOPBuffer(transStream)
	}

	return transStream, nil
}

func (t *transStreamPublisher) DispatchGOPBuffer(transStream TransStream) {
	if t.gopBuffer != nil {
		t.gopBuffer.PeekAll(func(packet *collections.ReferenceCounter[*avformat.AVPacket]) {
			t.DispatchPacketToStream(transStream, packet.Get())
		})

		// 发送转码包
		for _, track := range t.transcodeTracks {
			size := track.packets.Size()
			for i := 0; i < size; i++ {
				t.DispatchPacketToStream(transStream, track.packets.Peek(i))
			}
		}
	}
}

func (t *transStreamPublisher) DispatchPacket(packet *avformat.AVPacket) {
	for _, transStream := range t.transStreams {
		if TransStreamGBCascaded == transStream.GetProtocol() {
			continue
		}

		t.DispatchPacketToStream(transStream, packet)
	}
}

// DispatchPacketToStream 分发AVPacket
func (t *transStreamPublisher) DispatchPacketToStream(transStream TransStream, packet *avformat.AVPacket) {
	trackIndex, ok := transStream.FindMuxerTrackIndex(packet.Index)
	if !ok {
		return
	} else if !transStream.GetID().HasTrack(packet.Index) {
		return
	}

	data, timestamp, videoKey, err := transStream.Input(packet, trackIndex)
	if err != nil || len(data) < 1 {
		return
	}

	t.DispatchBuffer(transStream, trackIndex, data, timestamp, videoKey)
}

// DispatchBuffer 分发传输流
func (t *transStreamPublisher) DispatchBuffer(transStream TransStream, index int, data []*collections.ReferenceCounter[[]byte], timestamp int64, keyVideo bool) {
	sinks := t.transStreamSinks[transStream.GetID()]
	hasVideo := transStream.HasVideo()

	for _, sink := range sinks {

		if sink.GetSentPacketCount() < 1 {
			// 如果存在视频, 确保向sink发送的第一帧是关键帧
			if hasVideo && !keyVideo {
				continue
			}

			if extraData, _, _ := transStream.ReadExtraData(timestamp); len(extraData) > 0 {
				if ok := t.write(sink, index, extraData, timestamp, false); !ok {
					continue
				}
			}
		}

		if ok := t.write(sink, index, data, timestamp, keyVideo); !ok {
			continue
		}
	}
}

func (t *transStreamPublisher) DispatchSegments(transStream TransStream, segments []TransStreamSegment) {
	for _, segment := range segments {
		t.DispatchBuffer(transStream, segment.Index, segment.Data, segment.TS, segment.Key)
	}
}

func (t *transStreamPublisher) pendingSink(sink Sink) {
	log.Sugar.Errorf("向sink推流超时,关闭连接. %s-sink: %s source: %s", sink.GetProtocol().String(), sink.GetID(), t.source)
	go sink.Close()
}

// 向sink推流
func (t *transStreamPublisher) write(sink Sink, index int, data []*collections.ReferenceCounter[[]byte], timestamp int64, keyVideo bool) bool {
	err := sink.Write(index, data, timestamp, keyVideo)
	if err == nil {
		sink.IncreaseSentPacketCount()
		return true
	}

	// 推流超时, 可能是服务器或拉流端带宽不够、拉流端不读取数据等情况造成内核发送缓冲区满, 进而阻塞.
	// 直接关闭连接. 当然也可以将sink先挂起, 后续再继续推流.
	if _, ok := err.(transport.ZeroWindowSizeError); ok {
		t.pendingSink(sink)
	}

	return false
}

func (t *transStreamPublisher) writeSegments(sink Sink, segments []TransStreamSegment) {
	for _, segment := range segments {
		t.write(sink, segment.Index, segment.Data, segment.TS, segment.Key)
	}
}

// 创建sink需要的输出流
func (t *transStreamPublisher) doAddSink(sink Sink, resume bool) bool {
	// 暂时不考虑多路视频流，意味着只能1路视频流和多路音频流，同理originStreams和allStreams里面的Stream互斥. 同时多路音频流的Codec必须一致
	audioCodecId, videoCodecId := sink.DesiredAudioCodecId(), sink.DesiredVideoCodecId()
	audioTrack := t.originTracks.FindWithType(utils.AVMediaTypeAudio)
	videoTrack := t.originTracks.FindWithType(utils.AVMediaTypeVideo)

	disableAudio := audioTrack == nil
	disableVideo := videoTrack == nil || !sink.EnableVideo()
	if disableAudio && disableVideo {
		return false
	}

	// 不支持对期望编码的流封装. 降级
	if utils.AVCodecIdNONE != audioCodecId || utils.AVCodecIdNONE != videoCodecId {
		audioCodecId = utils.AVCodecIdNONE
		videoCodecId = utils.AVCodecIdNONE
	}

	if !disableAudio && utils.AVCodecIdNONE == audioCodecId {
		audioCodecId = audioTrack.Stream.CodecID
	}
	if !disableVideo && utils.AVCodecIdNONE == videoCodecId {
		videoCodecId = videoTrack.Stream.CodecID
	}

	// 创建音频转码器
	if !disableAudio && audioCodecId != audioTrack.Stream.CodecID {
		utils.Assert(false)
	}

	// 创建视频转码器
	if !disableVideo && videoCodecId != videoTrack.Stream.CodecID {
		utils.Assert(false)
	}

	// 查找传输流需要的所有track
	var tracks []*Track
	for _, track := range t.originTracks.All() {
		if disableVideo && track.Stream.MediaType == utils.AVMediaTypeVideo {
			continue
		}

		tracks = append(tracks, track)
	}

	var transStream TransStream
	var exist bool
	// 查找已经存在的传输流
	for _, stream := range t.transStreams {
		if stream.GetID().Protocol() == sink.GetProtocol() {
			transStream = stream
			exist = true
			break
		}
	}

	// 不存在创建新的传输流
	if !exist {
		var err error
		transStream, err = t.CreateTransStream(sink.GetProtocol(), tracks, sink)
		if err != nil {
			log.Sugar.Errorf("添加sink失败,创建传输流发生err: %s source: %s", err.Error(), t.source)
			return false
		}
	}

	sink.SetTransStreamID(transStream.GetID())

	{
		sink.Lock()
		if SessionStateClosed == sink.GetState() {
			sink.UnLock()
			log.Sugar.Warnf("添加sink失败, sink已经断开连接 %s", sink.String())
			return false
		} else {
			sink.SetState(SessionStateTransferring)
		}
		sink.UnLock()
	}

	// 开始推流
	err := sink.StartStreaming(transStream)
	if err != nil {
		log.Sugar.Errorf("添加sink失败,开始推流发生err: %s sink: %s source: %s ", err.Error(), SinkID2String(sink.GetID()), t.source)
		return false
	}

	// 还没做好准备(rtsp拉流还在协商sdp中), 暂不推流
	if !sink.IsReady() {
		return true
	}

	t.sinks[sink.GetID()] = sink
	t.transStreamSinks[transStream.GetID()][sink.GetID()] = sink

	// 累加拉流计数
	if !resume && t.recordSink != sink {
		t.sinkCount++
		log.Sugar.Infof("sink count: %d source: %s", t.sinkCount, t.source)
	}

	// TCP拉流开启异步发包, 一旦出现网络不好的链路, 其余正常链路不受影响.
	_, ok := sink.GetConn().(*transport.Conn)
	if ok && sink.IsTCPStreaming() {
		sink.EnableAsyncWriteMode(24)
	}

	// 发送已缓存的合并写切片
	segments, _ := transStream.ReadKeyFrameBuffer()
	if len(segments) > 0 {
		if extraData, _, _ := transStream.ReadExtraData(0); len(extraData) > 0 {
			t.write(sink, 0, extraData, 0, false)
		}

		t.writeSegments(sink, segments)
	}

	return true
}

func (t *transStreamPublisher) AddSink(sink Sink) {
	t.PostEvent(func() {
		if !t.completed.Load() {
			AddSinkToWaitingQueue(sink.GetSourceID(), sink)
		} else {
			if !t.doAddSink(sink, false) {
				go sink.Close()
			}
		}
	})
}

func (t *transStreamPublisher) RemoveSink(sink Sink) {
	t.ExecuteSyncEvent(func() {
		t.doRemoveSink(sink)
	})
}

func (t *transStreamPublisher) RemoveSinkWithID(id SinkID) {
	t.PostEvent(func() {
		sink, ok := t.sinks[id]
		if ok {
			t.doRemoveSink(sink)
		}
	})
}

func (t *transStreamPublisher) FindSink(id SinkID) Sink {
	var result Sink
	t.ExecuteSyncEvent(func() {
		sink, ok := t.sinks[id]
		if ok {
			result = sink
		}
	})

	return result
}

func (t *transStreamPublisher) clearSinkStreaming(sink Sink) {
	transStreamSinks := t.transStreamSinks[sink.GetTransStreamID()]
	delete(transStreamSinks, sink.GetID())
	t.lastStreamEndTime = time.Now()
	sink.StopStreaming(t.transStreams[sink.GetTransStreamID()])
	delete(t.sinks, sink.GetID())
}

func (t *transStreamPublisher) doRemoveSink(sink Sink) bool {
	if _, ok := t.sinks[sink.GetID()]; ok {
		t.clearSinkStreaming(sink)

		t.sinkCount--
		log.Sugar.Infof("sink count: %d source: %s", t.sinkCount, t.source)
		utils.Assert(t.sinkCount > -1)
	}

	HookPlayDoneEvent(sink)
	return true
}

func (t *transStreamPublisher) close() {
	t.ExecuteSyncEvent(func() {
		t.doClose()
	})
}

func (t *transStreamPublisher) doClose() {
	t.closed.Store(true)

	// 释放GOP缓存
	if t.gopBuffer != nil {
		t.ClearGopBuffer(true)
		t.gopBuffer = nil
	}

	// 关闭转码器
	for _, track := range t.transcodeTracks {
		track.Close()
	}

	// 关闭录制流
	if t.recordSink != nil {
		t.recordSink.Close()
	}

	// 保留推流信息
	if t.sinkCount > 0 && len(t.originTracks.All()) > 0 {
		var tracks []*Track
		for _, track := range t.originTracks.All() {
			tracks = append(tracks, track)
		}

		for _, track := range t.transcodeTracks {
			tracks = append(tracks, track.track)
		}

		sourceHistory := StreamEndInfoBride(t.source, t.originTracks.All(), t.transStreams)
		streamEndInfoManager.Add(sourceHistory)
	}

	// 关闭所有输出流
	for _, transStream := range t.transStreams {
		// 发送剩余包
		segments, _ := transStream.Close()
		if len(segments) > 0 {
			t.DispatchSegments(transStream, segments)
		}

		// 如果是tcp传输流, 归还合并写缓冲区
		if !transStream.IsTCPStreaming() || transStream.GetMWBuffer() == nil {
			continue
		} else if buffers := transStream.GetMWBuffer().Close(); buffers != nil {
			AddMWBuffersToPending(t.source, transStream.GetID(), buffers)
		}
	}

	// 将所有sink添加到等待队列
	for _, sink := range t.sinks {
		transStreamID := sink.GetTransStreamID()
		if t.recordSink == sink {
			continue
		}

		{
			sink.Lock()

			if SessionStateClosed == sink.GetState() {
				log.Sugar.Warnf("添加到sink到等待队列失败, sink已经断开连接 %s", sink.String())
			} else {
				sink.SetState(SessionStateWaiting)
				AddSinkToWaitingQueue(t.source, sink)
			}

			sink.UnLock()
		}

		if SessionStateClosed != sink.GetState() {
			sink.StopStreaming(t.transStreams[transStreamID])
		}
	}

	t.transStreams = nil
	t.sinks = nil
	t.transStreamSinks = nil
}

func (t *transStreamPublisher) WriteHeader() {
	t.completed.Store(true)

	// 尝试使用上次结束推流的时间戳
	if streamInfo := streamEndInfoManager.Remove(t.source); streamInfo != nil && EqualsTracks(streamInfo, t.originTracks.All()) {
		t.streamEndInfo = streamInfo
	}

	// 创建录制流和HLS
	t.CreateDefaultOutStreams()

	// 将等待队列的sink添加到输出流队列
	sinks := PopWaitingSinks(t.source)
	if t.recordSink != nil {
		sinks = append(sinks, t.recordSink)
	}

	for _, sink := range sinks {
		if !t.doAddSink(sink, false) {
			go sink.Close()
		}
	}

	// 如果不存在视频帧, 清空GOP缓存
	if !t.hasVideo {
		t.ClearGopBuffer(false)
		t.gopBuffer = nil
	}
}

func (t *transStreamPublisher) Sinks() []Sink {
	var sinks []Sink

	t.ExecuteSyncEvent(func() {
		for _, sink := range t.sinks {
			sinks = append(sinks, sink)
		}
	})

	return sinks
}

// ClearGopBuffer 清空GOP缓存, 在关闭stream publisher时, free为true, AVPacket放回池中. 如果free为false, 由Source放回池中.
func (t *transStreamPublisher) ClearGopBuffer(free bool) {
	t.gopBuffer.PopAll(func(packet *collections.ReferenceCounter[*avformat.AVPacket]) {
		if packet.Release() && free {
			avformat.FreePacket(packet.Get())
		}
	})

	// 释放annexb和avcc格式转换的缓存
	if t.bitstreamFilterBuffer != nil {
		t.bitstreamFilterBuffer.Clear()
	}

	// 丢弃转码track中的缓存
	for _, track := range t.transcodeTracks {
		track.Clear()
	}
}

func (t *transStreamPublisher) OnPacket(packet *collections.ReferenceCounter[*avformat.AVPacket]) {
	// 保存到GOP缓存
	if (AppConfig.GOPCache && t.hasVideo) || !t.completed.Load() {
		packet.Get().OnBufferAlloc = func(size int) []byte {
			if t.bitstreamFilterBuffer == nil {
				t.bitstreamFilterBuffer = collections.NewRBBlockBuffer(1024 * 1024 * 2)
			}

			return t.bitstreamFilterBuffer.Alloc(size)
		}

		// GOP队列溢出
		if t.gopBuffer.RequiresClear(packet) {
			t.ClearGopBuffer(false)
		}

		t.gopBuffer.AddPacket(packet)
	}

	// track解析完毕后，才能生成传输流
	if t.completed.Load() {
		// 分发给各个传输流
		t.DispatchPacket(packet.Get())

		// 转码
		for _, track := range t.transcodeTracks {
			transcodePackets := track.Input(packet.Get())
			for _, transcodePkt := range transcodePackets {
				//log.Sugar.Infof("packet dts: %d, pts: %d, t dts: %d, pts: %d", packet.Get().Dts, packet.Get().Pts, transcodePkt.Dts, transcodePkt.Pts)
				t.DispatchPacket(transcodePkt)
			}
		}

		// 未开启GOP缓存或只存在音频流, 立即释放
		if !AppConfig.GOPCache || !t.hasVideo {
			packet.Release()
		}
	}
}

func (t *transStreamPublisher) OnNewTrack(track *Track) {
	stream := track.Stream
	t.originTracks.Add(track)

	if utils.AVMediaTypeVideo == stream.MediaType {
		t.hasVideo = true
	}

	// 创建GOPBuffer
	if t.gopBuffer == nil {
		t.gopBuffer = NewStreamBuffer()
	}
}

func (t *transStreamPublisher) GetTransStreams() map[TransStreamID]TransStream {
	return t.transStreams
}

func (t *transStreamPublisher) GetStreamEndInfo() *StreamEndInfo {
	return t.streamEndInfo
}

func (t *transStreamPublisher) TranscodeTracks() []*Track {
	return t.originTracks.All()
}

func (t *transStreamPublisher) LastStreamEndTime() time.Time {
	return t.lastStreamEndTime
}

func (t *transStreamPublisher) SinkCount() int {
	return t.sinkCount
}

func (t *transStreamPublisher) GetForwardTransStream() TransStream {
	return t.forwardTransStream
}

func (t *transStreamPublisher) SetSourceID(id string) {
	t.source = id
}

func (t *transStreamPublisher) StartRecord() bool {
	if AppConfig.Record.Enable || t.recordSink != nil {
		return false
	}

	var ok bool
	t.ExecuteSyncEvent(func() {
		t.hasManualRecording = true
		// 如果探测还未结束
		if !t.completed.Load() {
			return
		}

		if t.recordSink == nil && t.createRecordSink() {
			ok = t.doAddSink(t.recordSink, false)
		}
	})

	return ok
}

func (t *transStreamPublisher) StopRecord() error {
	if AppConfig.Record.Enable {
		return fmt.Errorf("录制常开")
	}

	t.ExecuteSyncEvent(func() {
		t.hasManualRecording = false
		if t.recordSink != nil {
			t.clearSinkStreaming(t.recordSink)
			t.recordSink.Close()
			t.recordSink = nil
			t.recordFilePath = ""
			t.recordStartTime = time.Time{}
		}
	})

	return nil
}

func (t *transStreamPublisher) RecordStartTime() time.Time {
	return t.recordStartTime
}

func (t *transStreamPublisher) GetRecordStreamPlayUrl() string {
	return GenerateRecordStreamPlayUrl(filepath.ToSlash(t.recordFilePath))
}

func NewTransStreamPublisher(source string) TransStreamPublisher {
	return &transStreamPublisher{
		transStreams:     make(map[TransStreamID]TransStream),
		transStreamSinks: make(map[TransStreamID]map[SinkID]Sink),
		sinks:            make(map[SinkID]Sink),
		source:           source,
	}
}
