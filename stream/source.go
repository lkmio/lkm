package stream

import (
	"fmt"
	"github.com/lkmio/avformat"
	"github.com/lkmio/avformat/collections"
	"github.com/lkmio/lkm/log"
	"github.com/lkmio/transport"
	"net"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/transcode"
)

var (
	StreamEndInfoBride func(s Source) *StreamEndInfo
)

// Source 对推流源的封装
type Source interface {
	// GetID 返回SourceID
	GetID() string

	SetID(id string)

	// Input 输入推流数据
	Input(data []byte) error

	// GetType 返回推流类型
	GetType() SourceType

	SetType(sourceType SourceType)

	// OriginTracks 返回所有的推流track
	OriginTracks() []*Track

	// TranscodeTracks 返回所有的转码track
	TranscodeTracks() []*Track

	// AddSink 添加Sink, 在此之前请确保Sink已经握手、授权通过. 如果Source还未WriteHeader，先将Sink添加到等待队列.
	// 匹配拉流期望的编码器, 创建TransStream或向已经存在TransStream添加Sink
	AddSink(sink Sink)

	// RemoveSink 同步删除Sink
	RemoveSink(sink Sink)

	RemoveSinkWithID(id SinkID)

	FindSink(id SinkID) Sink

	SetState(state SessionState)

	// Close 关闭Source
	// 关闭推流网络链路, 停止一切封装和转发流以及转码工作
	// 将Sink添加到等待队列
	Close()

	// IsCompleted 所有推流track是否解析完毕
	IsCompleted() bool

	Init(receiveQueueSize int)

	RemoteAddr() string

	String() string

	State() SessionState

	// UrlValues 返回推流url参数
	UrlValues() url.Values

	// SetUrlValues 设置推流url参数
	SetUrlValues(values url.Values)

	// PostEvent 切换到主协程执行当前函数
	PostEvent(cb func())

	ExecuteSyncEvent(cb func())

	// LastPacketTime 返回最近收流时间戳
	LastPacketTime() time.Time

	SetLastPacketTime(time2 time.Time)

	// SinkCount 返回拉流计数
	SinkCount() int

	// LastStreamEndTime 返回最近结束拉流时间戳
	LastStreamEndTime() time.Time

	IsClosed() bool

	StreamPipe() chan []byte

	MainContextEvents() chan func()

	CreateTime() time.Time

	SetCreateTime(time time.Time)

	Sinks() []Sink

	GetBitrateStatistics() *BitrateStatistics

	GetTransStreams() map[TransStreamID]TransStream

	GetStreamEndInfo() *StreamEndInfo

	ProbeTimeout()
}

type PublishSource struct {
	ID    string
	Type  SourceType
	state SessionState
	Conn  net.Conn

	TransDemuxer     avformat.Demuxer       // 负责从推流协议中解析出AVStream和AVPacket
	recordSink       Sink                   // 每个Source的录制流
	recordFilePath   string                 // 录制流文件路径
	hlsStream        TransStream            // HLS传输流, 如果开启, 在@see writeHeader 函数中直接创建, 如果等拉流时再创建, 会进一步加大HLS延迟.
	audioTranscoders []transcode.Transcoder // 音频解码器
	videoTranscoders []transcode.Transcoder // 视频解码器
	originTracks     TrackManager           // 推流的音视频Streams
	allStreamTracks  TrackManager           // 推流Streams+转码器获得的Stream
	gopBuffer        GOPBuffer              // GOP缓存, 音频和视频混合使用, 以视频关键帧为界, 缓存第二个视频关键帧时, 释放前一组gop. 如果不存在视频流, 不缓存音频

	closed     atomic.Bool // source是否已经关闭
	completed  atomic.Bool // 所有推流track是否解析完毕, @see writeHeader 函数中赋值为true
	existVideo bool        // 是否存在视频

	TransStreams         map[TransStreamID]TransStream     // 所有输出流
	ForwardTransStream   TransStream                       // 转发流
	sinks                map[SinkID]Sink                   // 保存所有Sink
	TransStreamSinks     map[TransStreamID]map[SinkID]Sink // 输出流对应的Sink
	streamEndInfo        *StreamEndInfo                    // 之前推流源信息
	accumulateTimestamps bool                              // 是否累加时间戳
	timestampModeDecided bool                              // 是否已经决定使用推流的时间戳，或者累加时间戳

	streamPipe        chan []byte // 推流数据管道
	mainContextEvents chan func() // 切换到主协程执行函数的事件管道

	lastPacketTime    time.Time          // 最近收到推流包的时间
	lastStreamEndTime time.Time          // 最近拉流端结束拉流的时间
	sinkCount         int                // 拉流端计数
	urlValues         url.Values         // 推流url携带的参数
	createTime        time.Time          // source创建时间
	statistics        *BitrateStatistics // 码流统计
	streamLogger      avformat.OnUnpackStream2FileHandler
}

func (s *PublishSource) SetLastPacketTime(time2 time.Time) {
	s.lastPacketTime = time2
}

func (s *PublishSource) IsClosed() bool {
	return s.closed.Load()
}

func (s *PublishSource) StreamPipe() chan []byte {
	return s.streamPipe
}

func (s *PublishSource) MainContextEvents() chan func() {
	return s.mainContextEvents
}

func (s *PublishSource) LastStreamEndTime() time.Time {
	return s.lastStreamEndTime
}

func (s *PublishSource) LastPacketTime() time.Time {
	return s.lastPacketTime
}

func (s *PublishSource) SinkCount() int {
	return s.sinkCount
}

func (s *PublishSource) GetID() string {
	return s.ID
}

func (s *PublishSource) SetID(id string) {
	s.ID = id
}

func (s *PublishSource) Init(receiveQueueSize int) {
	s.SetState(SessionStateHandshakeSuccess)

	// 初始化事件接收管道
	// -2是为了保证从管道取到流, 到处理完流整个过程安全的, 不会被覆盖
	s.streamPipe = make(chan []byte, receiveQueueSize-2)
	s.mainContextEvents = make(chan func(), 128)

	s.TransStreams = make(map[TransStreamID]TransStream, 10)
	s.sinks = make(map[SinkID]Sink, 128)
	s.TransStreamSinks = make(map[TransStreamID]map[SinkID]Sink, len(transStreamFactories)+1)
	s.statistics = NewBitrateStatistics()
	// 设置探测时长
	s.TransDemuxer.SetProbeDuration(AppConfig.ProbeTimeout)
}

func (s *PublishSource) CreateDefaultOutStreams() {
	if s.TransStreams == nil {
		s.TransStreams = make(map[TransStreamID]TransStream, 10)
	}

	// 创建录制流
	if AppConfig.Record.Enable {
		sink, path, err := CreateRecordStream(s.ID)
		if err != nil {
			log.Sugar.Errorf("创建录制sink失败 source:%s err:%s", s.ID, err.Error())
		} else {
			s.recordSink = sink
			s.recordFilePath = path
		}
	}

	// 创建HLS输出流
	if AppConfig.Hls.Enable {
		streams := s.OriginTracks()
		utils.Assert(len(streams) > 0)

		id := GenerateTransStreamID(TransStreamHls, streams...)
		hlsStream, err := s.CreateTransStream(id, TransStreamHls, streams)
		if err != nil {
			panic(err)
		}

		s.DispatchGOPBuffer(hlsStream)
		s.hlsStream = hlsStream
		s.TransStreams[id] = s.hlsStream
	}
}

func (s *PublishSource) Input(data []byte) error {
	s.streamPipe <- data
	s.statistics.Input(len(data))
	return nil
}

func (s *PublishSource) OriginTracks() []*Track {
	return s.originTracks.All()
}

func (s *PublishSource) TranscodeTracks() []*Track {
	return s.allStreamTracks.All()
}

func IsSupportMux(protocol TransStreamProtocol, _, _ utils.AVCodecID) bool {
	if TransStreamRtmp == protocol || TransStreamFlv == protocol {

	}

	return true
}

func (s *PublishSource) CreateTransStream(id TransStreamID, protocol TransStreamProtocol, tracks []*Track) (TransStream, error) {
	log.Sugar.Infof("创建%s-stream source: %s", protocol.String(), s.ID)

	source := SourceManager.Find(s.ID)
	utils.Assert(source != nil)
	transStream, err := CreateTransStream(source, protocol, tracks)
	if err != nil {
		log.Sugar.Errorf("创建传输流失败 err: %s source: %s", err.Error(), s.ID)
		return nil, err
	}

	for _, track := range tracks {
		// 重新拷贝一个track，传输流内部使用track的时间戳，
		newTrack := *track
		if err = transStream.AddTrack(&newTrack); err != nil {
			return nil, err
		}
	}

	transStream.SetID(id)
	transStream.SetProtocol(protocol)

	// 创建输出流对应的拉流队列
	s.TransStreamSinks[id] = make(map[SinkID]Sink, 128)
	_ = transStream.WriteHeader()

	// 设置转发流
	if TransStreamGBStreamForward == transStream.GetProtocol() {
		s.ForwardTransStream = transStream
	}

	return transStream, err
}

func (s *PublishSource) DispatchGOPBuffer(transStream TransStream) {
	if s.gopBuffer != nil {
		s.gopBuffer.PeekAll(func(packet *avformat.AVPacket) {
			s.DispatchPacket(transStream, packet)
		})
	}
}

// DispatchPacket 分发AVPacket
func (s *PublishSource) DispatchPacket(transStream TransStream, packet *avformat.AVPacket) {
	data, timestamp, videoKey, err := transStream.Input(packet)
	if err != nil || len(data) < 1 {
		return
	}

	s.DispatchBuffer(transStream, packet.Index, data, timestamp, videoKey)
}

// DispatchBuffer 分发传输流
func (s *PublishSource) DispatchBuffer(transStream TransStream, index int, data []*collections.ReferenceCounter[[]byte], timestamp int64, keyVideo bool) {
	sinks := s.TransStreamSinks[transStream.GetID()]
	exist := transStream.IsExistVideo()

	for _, sink := range sinks {

		// 如果存在视频, 确保向sink发送的第一帧是关键帧
		if exist && sink.GetSentPacketCount() < 1 {
			if !keyVideo {
				continue
			}

			if extraData, _, _ := transStream.ReadExtraData(timestamp); len(extraData) > 0 {
				if ok := s.write(sink, index, extraData, timestamp, false); !ok {
					continue
				}
			}
		}

		if ok := s.write(sink, index, data, timestamp, keyVideo); !ok {
			continue
		}
	}
}

func (s *PublishSource) pendingSink(sink Sink) {
	log.Sugar.Errorf("向sink推流超时,关闭连接. %s-sink: %s source: %s", sink.GetProtocol().String(), sink.GetID(), s.ID)
	go sink.Close()
}

// 向sink推流
func (s *PublishSource) write(sink Sink, index int, data []*collections.ReferenceCounter[[]byte], timestamp int64, keyVideo bool) bool {
	err := sink.Write(index, data, timestamp, keyVideo)
	if err == nil {
		sink.IncreaseSentPacketCount()
		return true
	}

	// 推流超时, 可能是服务器或拉流端带宽不够、拉流端不读取数据等情况造成内核发送缓冲区满, 进而阻塞.
	// 直接关闭连接. 当然也可以将sink先挂起, 后续再继续推流.
	if _, ok := err.(transport.ZeroWindowSizeError); ok {
		s.pendingSink(sink)
	}

	return false
}

// 创建sink需要的输出流
func (s *PublishSource) doAddSink(sink Sink, resume bool) bool {
	// 暂时不考虑多路视频流，意味着只能1路视频流和多路音频流，同理originStreams和allStreams里面的Stream互斥. 同时多路音频流的Codec必须一致
	audioCodecId, videoCodecId := sink.DesiredAudioCodecId(), sink.DesiredVideoCodecId()
	audioTrack := s.originTracks.FindWithType(utils.AVMediaTypeAudio)
	videoTrack := s.originTracks.FindWithType(utils.AVMediaTypeVideo)

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
	for _, track := range s.originTracks.All() {
		if disableVideo && track.Stream.MediaType == utils.AVMediaTypeVideo {
			continue
		}

		tracks = append(tracks, track)
	}

	transStreamId := GenerateTransStreamID(sink.GetProtocol(), tracks...)
	transStream, exist := s.TransStreams[transStreamId]
	if !exist {
		var err error
		transStream, err = s.CreateTransStream(transStreamId, sink.GetProtocol(), tracks)
		if err != nil {
			log.Sugar.Errorf("添加sink失败,创建传输流发生err: %s source: %s", err.Error(), s.ID)
			return false
		}

		s.TransStreams[transStreamId] = transStream
	}

	sink.SetTransStreamID(transStreamId)

	{
		sink.Lock()
		defer sink.UnLock()

		if SessionStateClosed == sink.GetState() {
			log.Sugar.Warnf("添加sink失败, sink已经断开连接 %s", sink.String())
			return false
		} else {
			sink.SetState(SessionStateTransferring)
		}
	}

	err := sink.StartStreaming(transStream)
	if err != nil {
		log.Sugar.Errorf("添加sink失败,开始推流发生err: %s sink: %s source: %s ", err.Error(), SinkId2String(sink.GetID()), s.ID)
		return false
	}

	// 还没做好准备(rtsp拉流还在协商sdp中), 暂不推流
	if !sink.IsReady() {
		return true
	}

	// 累加拉流计数
	if !resume && s.recordSink != sink {
		s.sinkCount++
		log.Sugar.Infof("sink count: %d source: %s", s.sinkCount, s.ID)
	}

	s.sinks[sink.GetID()] = sink
	s.TransStreamSinks[transStreamId][sink.GetID()] = sink

	// TCP拉流开启异步发包, 一旦出现网络不好的链路, 其余正常链路不受影响.
	_, ok := sink.GetConn().(*transport.Conn)
	if ok && sink.IsTCPStreaming() {
		sink.EnableAsyncWriteMode(24)
	}

	// 发送已有的缓存数据
	// 此处发送缓存数据，必须要存在关键帧的输出流才发，否则等DispatchPacket时再发送extra。
	data, timestamp, _ := transStream.ReadKeyFrameBuffer()
	if len(data) > 0 {
		if extraData, _, _ := transStream.ReadExtraData(timestamp); len(extraData) > 0 {
			s.write(sink, 0, extraData, timestamp, false)
		}

		s.write(sink, 0, data, timestamp, true)
	}

	// 新建传输流，发送已经缓存的音视频帧
	if !exist && AppConfig.GOPCache && s.existVideo {
		s.DispatchGOPBuffer(transStream)
	}

	return true
}

func (s *PublishSource) AddSink(sink Sink) {
	s.PostEvent(func() {
		if !s.completed.Load() {
			AddSinkToWaitingQueue(sink.GetSourceID(), sink)
		} else {
			if !s.doAddSink(sink, false) {
				go sink.Close()
			}
		}
	})
}

func (s *PublishSource) RemoveSink(sink Sink) {
	s.ExecuteSyncEvent(func() {
		s.doRemoveSink(sink)
	})
}

func (s *PublishSource) RemoveSinkWithID(id SinkID) {
	s.PostEvent(func() {
		sink, ok := s.sinks[id]
		if ok {
			s.doRemoveSink(sink)
		}
	})
}

func (s *PublishSource) FindSink(id SinkID) Sink {
	var result Sink
	s.ExecuteSyncEvent(func() {
		sink, ok := s.sinks[id]
		if ok {
			result = sink
		}
	})

	return result
}

func (s *PublishSource) cleanupSinkStreaming(sink Sink) {
	transStreamSinks := s.TransStreamSinks[sink.GetTransStreamID()]
	delete(transStreamSinks, sink.GetID())
	s.lastStreamEndTime = time.Now()

	if sink.GetProtocol() == TransStreamHls {
		// 从HLS拉流队列删除Sink
		_, _ = SinkManager.Remove(sink.GetID())
	}

	sink.StopStreaming(s.TransStreams[sink.GetTransStreamID()])
}

func (s *PublishSource) doRemoveSink(sink Sink) bool {
	s.cleanupSinkStreaming(sink)
	delete(s.sinks, sink.GetID())

	s.sinkCount--
	log.Sugar.Infof("sink count: %d source: %s", s.sinkCount, s.ID)
	utils.Assert(s.sinkCount > -1)

	HookPlayDoneEvent(sink)
	return true
}

func (s *PublishSource) SetState(state SessionState) {
	s.state = state
}

func (s *PublishSource) DoClose() {
	log.Sugar.Debugf("closing the %s source. id: %s. closed flag: %t", s.Type, s.ID, s.closed.Load())

	if s.closed.Load() {
		return
	}

	s.closed.Store(true)

	// 释放GOP缓存
	if s.gopBuffer != nil {
		s.gopBuffer.PopAll(func(packet *avformat.AVPacket) {
			s.TransDemuxer.DiscardHeadPacket(packet.BufferIndex)
		})
		s.gopBuffer = nil
	}

	// 关闭推流源的解复用器
	if s.TransDemuxer != nil {
		s.TransDemuxer.Close()
		s.TransDemuxer = nil
	}

	// 关闭录制流
	if s.recordSink != nil {
		s.recordSink.Close()
	}

	// 释放解复用器
	// 释放转码器
	// 释放每路转协议流， 将所有sink添加到等待队列
	_, err := SourceManager.Remove(s.ID)
	if err != nil {
		// source不存在, 在创建source时, 未添加到manager中, 目前只有1078流会出现这种情况(tcp连接到端口, 没有推流或推流数据无效, 无法定位到手机号, 以至于无法执行PreparePublishSource函数), 将不再处理后续事情.
		log.Sugar.Errorf("删除源失败 source:%s err:%s", s.ID, err.Error())
		return
	}

	// 保留推流信息
	if s.sinkCount > 0 && len(s.originTracks.All()) > 0 {
		sourceHistory := StreamEndInfoBride(s)
		streamEndInfoManager.Add(sourceHistory)
	}

	// 关闭所有输出流
	for _, transStream := range s.TransStreams {
		// 发送剩余包
		data, ts, _ := transStream.Close()
		if len(data) > 0 {
			s.DispatchBuffer(transStream, -1, data, ts, true)
		}

		// 如果是tcp传输流, 归还合并写缓冲区
		if !transStream.IsTCPStreaming() || transStream.GetMWBuffer() == nil {
			continue
		} else if buffers := transStream.GetMWBuffer().Close(); buffers != nil {
			AddMWBuffersToPending(s.ID, transStream.GetID(), buffers)
		}
	}

	// 将所有sink添加到等待队列
	for _, sink := range s.sinks {
		transStreamID := sink.GetTransStreamID()
		sink.SetTransStreamID(0)
		if s.recordSink == sink {
			return
		}

		{
			sink.Lock()

			if SessionStateClosed == sink.GetState() {
				log.Sugar.Warnf("添加到sink到等待队列失败, sink已经断开连接 %s", sink.String())
			} else {
				sink.SetState(SessionStateWaiting)
				AddSinkToWaitingQueue(s.ID, sink)
			}

			sink.UnLock()
		}

		if SessionStateClosed != sink.GetState() {
			sink.StopStreaming(s.TransStreams[transStreamID])
		}
	}

	s.TransStreams = nil
	s.sinks = nil
	s.TransStreamSinks = nil

	// 异步hook
	go func() {
		if s.Conn != nil {
			_ = s.Conn.Close()
			s.Conn = nil
		}

		HookPublishDoneEvent(s)

		if s.recordSink != nil {
			HookRecordEvent(s, s.recordFilePath)
		}
	}()
}

func (s *PublishSource) Close() {
	if s.closed.Load() {
		return
	}

	// 同步执行, 确保close后, 主协程已经退出, 不会再处理任何推拉流、查询等任何事情.
	s.ExecuteSyncEvent(func() {
		s.DoClose()
	})
}

// 解析完所有track后, 创建各种输出流
func (s *PublishSource) writeHeader() {
	if s.completed.Load() {
		fmt.Printf("添加Stream失败 Source: %s已经WriteHeader", s.ID)
		return
	}

	s.completed.Store(true)

	if len(s.originTracks.All()) == 0 {
		log.Sugar.Errorf("没有一路track, 删除source: %s", s.ID)
		s.DoClose()
		return
	}

	// 尝试恢复上次推流的会话
	if streamInfo := streamEndInfoManager.Remove(s.ID); streamInfo != nil && EqualsTracks(streamInfo, s.originTracks.All()) {
		s.streamEndInfo = streamInfo

		// 恢复每路track的时间戳
		tracks := s.originTracks.All()
		for _, track := range tracks {
			timestamps := streamInfo.Timestamps[track.Stream.CodecID]
			track.Dts = timestamps[0]
			track.Pts = timestamps[1]
		}
	}

	// 纠正GOP中的时间戳
	if s.gopBuffer != nil && s.gopBuffer.Size() != 0 {
		s.gopBuffer.PeekAll(func(packet *avformat.AVPacket) {
			s.CorrectTimestamp(packet)
		})
	}

	// 创建录制流和HLS
	s.CreateDefaultOutStreams()

	// 将等待队列的sink添加到输出流队列
	sinks := PopWaitingSinks(s.ID)
	if s.recordSink != nil {
		sinks = append(sinks, s.recordSink)
	}

	for _, sink := range sinks {
		if !s.doAddSink(sink, false) {
			go sink.Close()
		}
	}
}

func (s *PublishSource) IsCompleted() bool {
	return s.completed.Load()
}

// NotTrackAdded 返回该index对应的track是否没有添加
func (s *PublishSource) NotTrackAdded(index int) bool {
	for _, track := range s.originTracks.All() {
		if track.Stream.Index == index {
			return false
		}
	}

	return true
}

func (s *PublishSource) CorrectTimestamp(packet *avformat.AVPacket) {
	// 对比第一包的时间戳和上次推流的最后时间戳。如果小于上次的推流时间戳，则在原来的基础上累加。
	if s.streamEndInfo != nil && !s.timestampModeDecided {
		s.timestampModeDecided = true

		timestamps := s.streamEndInfo.Timestamps[packet.CodecID]
		s.accumulateTimestamps = true
		log.Sugar.Infof("累加时间戳 上次推流dts: %d, pts: %d", timestamps[0], timestamps[1])
	}

	track := s.originTracks.Find(packet.CodecID)
	duration := packet.GetDuration(packet.Timebase)

	// 根据duration来累加时间戳
	if s.accumulateTimestamps {
		offset := packet.Pts - packet.Dts
		packet.Dts = track.Dts + duration
		packet.Pts = packet.Dts + offset
	}

	track.Dts = packet.Dts
	track.Pts = packet.Pts
	track.FrameDuration = int(duration)
}

func (s *PublishSource) OnNewTrack(track avformat.Track) {
	if AppConfig.Debug {
		s.streamLogger.Path = "dump/" + strings.ReplaceAll(s.ID, "/", "_")
		s.streamLogger.OnNewTrack(track)
	}

	stream := track.GetStream()

	if s.completed.Load() {
		log.Sugar.Warnf("添加%s track失败,已经WriteHeader. source: %s", stream.MediaType, s.ID)
		return
	} else if !s.NotTrackAdded(stream.Index) {
		log.Sugar.Warnf("添加%s track失败,已经添加索引为%d的track. source: %s", stream.MediaType, stream.Index, s.ID)
		return
	}

	s.originTracks.Add(NewTrack(stream, 0, 0))
	s.allStreamTracks.Add(NewTrack(stream, 0, 0))

	if utils.AVMediaTypeVideo == stream.MediaType {
		s.existVideo = true
	}

	// 创建GOPBuffer
	if AppConfig.GOPCache && s.existVideo && s.gopBuffer == nil {
		s.gopBuffer = NewStreamBuffer()
	}
}

func (s *PublishSource) OnTrackComplete() {
	if AppConfig.Debug {
		s.streamLogger.OnTrackComplete()
	}

	s.writeHeader()
}

func (s *PublishSource) OnTrackNotFind() {
	if AppConfig.Debug {
		s.streamLogger.OnTrackNotFind()
	}

	log.Sugar.Errorf("no tracks found. source id: %s", s.ID)
}

func (s *PublishSource) OnPacket(packet *avformat.AVPacket) {
	if AppConfig.Debug {
		s.streamLogger.OnPacket(packet)
	}

	// track超时，忽略推流数据
	if s.NotTrackAdded(packet.Index) {
		s.TransDemuxer.DiscardBackPacket(packet.BufferIndex)
		return
	}

	// 保存到GOP缓存
	if AppConfig.GOPCache && s.existVideo {
		// GOP队列溢出
		if s.gopBuffer.RequiresClear(packet) {
			s.gopBuffer.PopAll(func(packet *avformat.AVPacket) {
				s.TransDemuxer.DiscardHeadPacket(packet.BufferIndex)
			})
		}

		s.gopBuffer.AddPacket(packet)
	}

	// track解析完毕后，才能生成传输流
	if s.completed.Load() {
		s.CorrectTimestamp(packet)

		// 分发给各个传输流
		for _, transStream := range s.TransStreams {
			s.DispatchPacket(transStream, packet)
		}
	}

	// 未开启GOP缓存或只存在音频流, 释放掉内存
	if !AppConfig.GOPCache || !s.existVideo {
		s.TransDemuxer.DiscardBackPacket(packet.BufferIndex)
	}
}

func (s *PublishSource) GetType() SourceType {
	return s.Type
}

func (s *PublishSource) SetType(sourceType SourceType) {
	s.Type = sourceType
}

func (s *PublishSource) RemoteAddr() string {
	if s.Conn == nil {
		return ""
	}

	return s.Conn.RemoteAddr().String()
}

func (s *PublishSource) String() string {
	return fmt.Sprintf("source: %s type: %s conn: %s ", s.ID, s.Type.String(), s.RemoteAddr())
}

func (s *PublishSource) State() SessionState {
	return s.state
}

func (s *PublishSource) UrlValues() url.Values {
	return s.urlValues
}

func (s *PublishSource) SetUrlValues(values url.Values) {
	s.urlValues = values
}

func (s *PublishSource) PostEvent(cb func()) {
	s.mainContextEvents <- cb
}

func (s *PublishSource) ExecuteSyncEvent(cb func()) {
	group := sync.WaitGroup{}
	group.Add(1)

	s.PostEvent(func() {
		cb()
		group.Done()
	})

	group.Wait()
}

func (s *PublishSource) CreateTime() time.Time {
	return s.createTime
}

func (s *PublishSource) SetCreateTime(time time.Time) {
	s.createTime = time
}

func (s *PublishSource) Sinks() []Sink {
	var sinks []Sink

	s.ExecuteSyncEvent(func() {
		for _, sink := range s.sinks {
			sinks = append(sinks, sink)
		}
	})

	return sinks
}

func (s *PublishSource) GetBitrateStatistics() *BitrateStatistics {
	return s.statistics
}

func (s *PublishSource) GetTransStreams() map[TransStreamID]TransStream {
	return s.TransStreams
}

func (s *PublishSource) GetStreamEndInfo() *StreamEndInfo {
	return s.streamEndInfo
}

func (s *PublishSource) ProbeTimeout() {
	if s.TransDemuxer != nil {
		s.TransDemuxer.ProbeComplete()
	}
}
