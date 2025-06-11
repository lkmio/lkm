package stream

import (
	"fmt"
	"github.com/lkmio/avformat"
	"github.com/lkmio/avformat/collections"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/log"
	"github.com/lkmio/lkm/transcode"
	"github.com/lkmio/transport"
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

	run()

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
}

type transStreamPublisher struct {
	source            string
	streamEvents      *NonBlockingChannel[*StreamEvent]
	mainContextEvents chan func()

	sinkCount int
	gopBuffer GOPBuffer // GOP缓存, 音频和视频混合使用, 以视频关键帧为界, 缓存第二个视频关键帧时, 释放前一组gop

	recordSink      Sink                   // 每个Source的录制流
	recordFilePath  string                 // 录制流文件路径
	hlsStream       TransStream            // HLS传输流, 如果开启, 在@see writeHeader 函数中直接创建, 如果等拉流时再创建, 会进一步加大HLS延迟.
	_               []transcode.Transcoder // 音频解码器
	_               []transcode.Transcoder // 视频解码器
	originTracks    TrackManager           // 推流的音视频Streams
	allStreamTracks TrackManager           // 推流Streams+转码器获得的Stream

	transStreams       map[TransStreamID]TransStream     // 所有输出流
	forwardTransStream TransStream                       // 转发流
	sinks              map[SinkID]Sink                   // 保存所有Sink
	transStreamSinks   map[TransStreamID]map[SinkID]Sink // 输出流对应的Sink

	existVideo            bool        // 是否存在视频
	completed             atomic.Bool // 所有推流track是否解析完毕, @see writeHeader 函数中赋值为true
	closed                atomic.Bool
	streamEndInfo         *StreamEndInfo             // 之前推流源信息
	accumulateTimestamps  bool                       // 是否累加时间戳
	timestampModeDecided  bool                       // 是否已经决定使用推流的时间戳，或者累加时间戳
	lastStreamEndTime     time.Time                  // 最近拉流端结束拉流的时间
	bitstreamFilterBuffer *collections.RBBlockBuffer // annexb和avcc转换的缓冲区
}

func (t *transStreamPublisher) Post(event *StreamEvent) {
	t.streamEvents.Post(event)
}

func (t *transStreamPublisher) run() {
	t.streamEvents = NewNonBlockingChannel[*StreamEvent](256)
	t.mainContextEvents = make(chan func(), 256)

	t.transStreams = make(map[TransStreamID]TransStream, 10)
	t.sinks = make(map[SinkID]Sink, 128)
	t.transStreamSinks = make(map[TransStreamID]map[SinkID]Sink, len(transStreamFactories)+1)

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
						t.DispatchPacket(t.forwardTransStream, &avformat.AVPacket{Data: data[2:]})
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

func (t *transStreamPublisher) PostEvent(cb func()) {
	t.mainContextEvents <- cb
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

func (t *transStreamPublisher) CreateDefaultOutStreams() {
	if t.transStreams == nil {
		t.transStreams = make(map[TransStreamID]TransStream, 10)
	}

	// 创建录制流
	if AppConfig.Record.Enable {
		sink, path, err := CreateRecordStream(t.source)
		if err != nil {
			log.Sugar.Errorf("创建录制sink失败 source: %s err: %s", t.source, err.Error())
		} else {
			t.recordSink = sink
			t.recordFilePath = path
		}
	}

	// 创建HLS输出流
	if AppConfig.Hls.Enable {
		streams := t.originTracks.All()
		utils.Assert(len(streams) > 0)

		id := GenerateTransStreamID(TransStreamHls, streams...)
		hlsStream, err := t.CreateTransStream(id, TransStreamHls, streams, nil)
		if err != nil {
			log.Sugar.Errorf("创建HLS输出流失败 source: %s err: %s", t.source, err.Error())
			return
		}

		t.DispatchGOPBuffer(hlsStream)
		t.hlsStream = hlsStream
		t.transStreams[id] = t.hlsStream
	}
}

func IsSupportMux(protocol TransStreamProtocol, _, _ utils.AVCodecID) bool {
	if TransStreamRtmp == protocol || TransStreamFlv == protocol {

	}

	return true
}

func (t *transStreamPublisher) CreateTransStream(id TransStreamID, protocol TransStreamProtocol, tracks []*Track, sink Sink) (TransStream, error) {
	log.Sugar.Infof("创建%s-stream source: %s", protocol.String(), t.source)

	source := SourceManager.Find(t.source)
	utils.Assert(source != nil)
	transStream, err := CreateTransStream(source, protocol, tracks, sink)
	if err != nil {
		return nil, err
	}

	for _, track := range tracks {
		supportedCodecs, ok := SupportedCodes[protocol]
		if !ok {
			panic(fmt.Sprintf("unknown protocol %s", protocol.String()))
		}

		_, ok = supportedCodecs[track.Stream.CodecID]
		if !ok {
			log.Sugar.Warnf("不支持的编码器 %s %s", protocol.String(), track.Stream.CodecID)
		}

		var index int
		// 重新拷贝一个track，传输流内部使用track的时间戳，
		newTrack := *track
		if index, err = transStream.AddTrack(&newTrack); err != nil {
			log.Sugar.Errorf("添加track失败 err: %s source: %s stream: %s, codec: %s ", err.Error(), t.source, protocol, track.Stream.CodecID)
			continue
		}

		// stream index->muxer track index
		transStream.SetMuxerTrack(index, &newTrack)
	}

	if transStream.TrackSize() == 0 {
		return nil, fmt.Errorf("not found track")
	}

	transStream.SetID(id)
	transStream.SetProtocol(protocol)

	// 创建输出流对应的拉流队列
	t.transStreamSinks[id] = make(map[SinkID]Sink, 128)
	_ = transStream.WriteHeader()

	// 设置转发流
	if TransStreamGBCascaded == transStream.GetProtocol() {
		t.forwardTransStream = transStream
	}

	return transStream, err
}

func (t *transStreamPublisher) DispatchGOPBuffer(transStream TransStream) {
	if t.gopBuffer != nil {
		t.gopBuffer.PeekAll(func(packet *collections.ReferenceCounter[*avformat.AVPacket]) {
			t.DispatchPacket(transStream, packet.Get())
		})
	}
}

// DispatchPacket 分发AVPacket
func (t *transStreamPublisher) DispatchPacket(transStream TransStream, packet *avformat.AVPacket) {
	trackIndex, ok := transStream.FindMuxerTrackIndex(packet.Index)
	if !ok {
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
	exist := transStream.IsExistVideo()

	for _, sink := range sinks {

		if sink.GetSentPacketCount() < 1 {
			// 如果存在视频, 确保向sink发送的第一帧是关键帧
			if exist && !keyVideo {
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
	if (utils.AVCodecIdNONE != audioCodecId || utils.AVCodecIdNONE != videoCodecId) && !IsSupportMux(sink.GetProtocol(), audioCodecId, videoCodecId) {
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

	transStreamId := GenerateTransStreamID(sink.GetProtocol(), tracks...)
	transStream, exist := t.transStreams[transStreamId]
	if !exist {
		var err error
		transStream, err = t.CreateTransStream(transStreamId, sink.GetProtocol(), tracks, sink)
		if err != nil {
			log.Sugar.Errorf("添加sink失败,创建传输流发生err: %s source: %s", err.Error(), t.source)
			return false
		}

		t.transStreams[transStreamId] = transStream
	}

	sink.SetTransStreamID(transStreamId)

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

	err := sink.StartStreaming(transStream)
	if err != nil {
		log.Sugar.Errorf("添加sink失败,开始推流发生err: %s sink: %s source: %s ", err.Error(), SinkID2String(sink.GetID()), t.source)
		return false
	}

	// 还没做好准备(rtsp拉流还在协商sdp中), 暂不推流
	if !sink.IsReady() {
		return true
	}

	// 累加拉流计数
	if !resume && t.recordSink != sink {
		t.sinkCount++
		log.Sugar.Infof("sink count: %d source: %s", t.sinkCount, t.source)
	}

	t.sinks[sink.GetID()] = sink
	t.transStreamSinks[transStreamId][sink.GetID()] = sink

	// TCP拉流开启异步发包, 一旦出现网络不好的链路, 其余正常链路不受影响.
	_, ok := sink.GetConn().(*transport.Conn)
	if ok && sink.IsTCPStreaming() {
		sink.EnableAsyncWriteMode(24)
	}

	// 发送已有的缓存数据
	// 此处发送缓存数据，必须要存在关键帧的输出流才发，否则等DispatchPacket时再发送extra。
	keyBuffer, timestamp, _ := transStream.ReadKeyFrameBuffer()
	if len(keyBuffer) > 0 {
		if extraData, _, _ := transStream.ReadExtraData(timestamp); len(extraData) > 0 {
			t.write(sink, 0, extraData, timestamp, false)
		}

		t.write(sink, 0, keyBuffer, timestamp, true)
	}

	// 新建传输流，发送已经缓存的音视频帧
	if !exist && AppConfig.GOPCache && t.existVideo && TransStreamGBCascaded != transStream.GetProtocol() {
		t.DispatchGOPBuffer(transStream)
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
}

func (t *transStreamPublisher) doRemoveSink(sink Sink) bool {
	t.clearSinkStreaming(sink)
	delete(t.sinks, sink.GetID())

	t.sinkCount--
	log.Sugar.Infof("sink count: %d source: %s", t.sinkCount, t.source)
	utils.Assert(t.sinkCount > -1)

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

	// 关闭录制流
	if t.recordSink != nil {
		t.recordSink.Close()
	}

	// 保留推流信息
	if t.sinkCount > 0 && len(t.originTracks.All()) > 0 {
		sourceHistory := StreamEndInfoBride(t.source, t.originTracks.All(), t.transStreams)
		streamEndInfoManager.Add(sourceHistory)
	}

	// 关闭所有输出流
	for _, transStream := range t.transStreams {
		// 发送剩余包
		data, ts, _ := transStream.Close()
		if len(data) > 0 {
			t.DispatchBuffer(transStream, -1, data, ts, true)
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
		sink.SetTransStreamID(0)
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

		// 恢复每路track的时间戳
		tracks := t.originTracks.All()
		for _, track := range tracks {
			timestamps := streamInfo.Timestamps[track.Stream.CodecID]
			track.Dts = timestamps[0]
			track.Pts = timestamps[1]
		}
	}

	// 纠正GOP中的时间戳
	if t.gopBuffer != nil && t.gopBuffer.Size() != 0 {
		t.gopBuffer.PeekAll(func(packet *collections.ReferenceCounter[*avformat.AVPacket]) {
			t.CorrectTimestamp(packet.Get())
		})
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
	if !t.existVideo {
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

		// 释放annexb和avcc格式转换的缓存
		if t.bitstreamFilterBuffer != nil {
			t.bitstreamFilterBuffer.Pop()
		}
	})
}

func (t *transStreamPublisher) OnPacket(packet *collections.ReferenceCounter[*avformat.AVPacket]) {
	// 保存到GOP缓存
	if (AppConfig.GOPCache && t.existVideo) || !t.completed.Load() {
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
		t.CorrectTimestamp(packet.Get())

		// 分发给各个传输流
		for _, transStream := range t.transStreams {
			if TransStreamGBCascaded != transStream.GetProtocol() {
				t.DispatchPacket(transStream, packet.Get())
			}
		}

		// 未开启GOP缓存或只存在音频流, 立即释放
		if !AppConfig.GOPCache || !t.existVideo {
			packet.Release()
		}
	}
}

func (t *transStreamPublisher) OnNewTrack(track *Track) {
	stream := track.Stream
	t.originTracks.Add(track)

	if utils.AVMediaTypeVideo == stream.MediaType {
		t.existVideo = true
	}

	// 创建GOPBuffer
	if t.gopBuffer == nil {
		t.gopBuffer = NewStreamBuffer()
	}
}

// CorrectTimestamp 纠正时间戳
func (t *transStreamPublisher) CorrectTimestamp(packet *avformat.AVPacket) {
	// 对比第一包的时间戳和上次推流的最后时间戳。如果小于上次的推流时间戳，则在原来的基础上累加。
	if t.streamEndInfo != nil && !t.timestampModeDecided {
		t.timestampModeDecided = true

		timestamps := t.streamEndInfo.Timestamps[packet.CodecID]
		t.accumulateTimestamps = true
		log.Sugar.Infof("累加时间戳 上次推流dts: %d, pts: %d", timestamps[0], timestamps[1])
	}

	track := t.originTracks.Find(packet.CodecID)
	duration := packet.GetDuration(packet.Timebase)

	// 根据duration来累加时间戳
	if t.accumulateTimestamps {
		offset := packet.Pts - packet.Dts
		packet.Dts = track.Dts + duration
		packet.Pts = packet.Dts + offset
	}

	track.Dts = packet.Dts
	track.Pts = packet.Pts
	track.FrameDuration = int(duration)
}

func (t *transStreamPublisher) GetTransStreams() map[TransStreamID]TransStream {
	return t.transStreams
}

func (t *transStreamPublisher) GetStreamEndInfo() *StreamEndInfo {
	return t.streamEndInfo
}

func (t *transStreamPublisher) TranscodeTracks() []*Track {
	return t.allStreamTracks.All()
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

func NewTransStreamPublisher(source string) TransStreamPublisher {
	return &transStreamPublisher{
		transStreams:     make(map[TransStreamID]TransStream),
		transStreamSinks: make(map[TransStreamID]map[SinkID]Sink),
		sinks:            make(map[SinkID]Sink),
		source:           source,
	}
}
