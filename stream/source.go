package stream

import (
	"fmt"
	"github.com/lkmio/avformat"
	"github.com/lkmio/avformat/collections"
	"github.com/lkmio/lkm/log"
	"net"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lkmio/avformat/utils"
)

var (
	StreamEndInfoBride func(source string, tracks []*Track, streams map[TransStreamID]TransStream) *StreamEndInfo
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
	postEvent(cb func())

	executeSyncEvent(cb func())

	// LastPacketTime 返回最近收流时间戳
	LastPacketTime() time.Time

	SetLastPacketTime(time2 time.Time)

	IsClosed() bool

	StreamPipe() chan []byte

	MainContextEvents() chan func()

	CreateTime() time.Time

	SetCreateTime(time time.Time)

	GetBitrateStatistics() *BitrateStatistics

	ProbeTimeout()

	GetTransStreamPublisher() TransStreamPublisher
}

type PublishSource struct {
	ID    string
	Type  SourceType
	state SessionState
	Conn  net.Conn

	streamPipe        *NonBlockingChannel[[]byte] // 推流数据管道
	mainContextEvents chan func()                 // 切换到主协程执行函数的事件管道
	streamPublisher   TransStreamPublisher        // 解析出来的AVStream和AVPacket, 交由streamPublisher处理

	TransDemuxer avformat.Demuxer // 负责从推流协议中解析出AVStream和AVPacket
	originTracks TrackManager     // 推流的音视频Streams

	closed     atomic.Bool // 是否已经关闭
	completed  atomic.Bool // 推流track是否解析完毕, @see writeHeader 函数中赋值为true
	existVideo bool        // 是否存在视频

	lastPacketTime time.Time          // 最近收到推流包的时间
	urlValues      url.Values         // 推流url携带的参数
	createTime     time.Time          // source创建时间
	statistics     *BitrateStatistics // 码流统计
	streamLogger   avformat.OnUnpackStream2FileHandler
}

func (s *PublishSource) SetLastPacketTime(time2 time.Time) {
	s.lastPacketTime = time2
}

func (s *PublishSource) IsClosed() bool {
	return s.closed.Load()
}

func (s *PublishSource) StreamPipe() chan []byte {
	return s.streamPipe.Channel
}

func (s *PublishSource) MainContextEvents() chan func() {
	return s.mainContextEvents
}

func (s *PublishSource) LastPacketTime() time.Time {
	return s.lastPacketTime
}

func (s *PublishSource) GetID() string {
	return s.ID
}

func (s *PublishSource) SetID(id string) {
	s.ID = id
	if s.streamPublisher != nil {
		s.streamPublisher.SetSourceID(id)
	}
}

func (s *PublishSource) Init(receiveQueueSize int) {
	s.SetState(SessionStateHandshakeSuccess)

	// 初始化事件接收管道
	// -2是为了保证从管道取到流, 到处理完流整个过程安全的, 不会被覆盖
	s.streamPipe = NewNonBlockingChannel[[]byte](receiveQueueSize - 1)
	s.mainContextEvents = make(chan func(), 128)
	s.statistics = NewBitrateStatistics()
	s.streamPublisher = NewTransStreamPublisher(s.ID)
	// 设置探测时长
	s.TransDemuxer.SetProbeDuration(AppConfig.ProbeTimeout)
}

func (s *PublishSource) Input(data []byte) error {
	s.streamPipe.Post(data)
	s.statistics.Input(len(data))
	return nil
}

func (s *PublishSource) OriginTracks() []*Track {
	return s.originTracks.All()
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

	// 关闭推流源的解复用器, 不再接收数据
	if s.TransDemuxer != nil {
		s.TransDemuxer.Close()
		s.TransDemuxer = nil
	}

	// 等传输流发布器关闭结束
	s.streamPublisher.close()

	// 释放解复用器
	// 释放转码器
	// 释放每路转协议流， 将所有sink添加到等待队列
	_, err := SourceManager.Remove(s.ID)
	if err != nil {
		// source不存在, 在创建source时, 未添加到manager中, 目前只有1078流会出现这种情况(tcp连接到端口, 没有推流或推流数据无效, 无法定位到手机号, 以至于无法执行PreparePublishSource函数), 将不再处理后续事情.
		log.Sugar.Errorf("删除源失败 source: %s err: %s", s.ID, err.Error())
		return
	}

	// 异步hook
	go func() {
		if s.Conn != nil {
			_ = s.Conn.Close()
			s.Conn = nil
		}

		HookPublishDoneEvent(s)
	}()
}

func (s *PublishSource) Close() {
	if s.closed.Load() {
		return
	}

	// 同步执行, 确保close后, 主协程已经退出, 不会再处理任何推拉流、查询等任何事情.
	s.executeSyncEvent(func() {
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

	s.streamPublisher.Post(&StreamEvent{StreamEventTypeTrackCompleted, nil})

	if len(s.originTracks.All()) == 0 {
		log.Sugar.Errorf("没有一路track, 删除source: %s", s.ID)
		s.DoClose()
		return
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

	newTrack := NewTrack(stream, 0, 0)
	s.originTracks.Add(newTrack)

	if utils.AVMediaTypeVideo == stream.MediaType {
		s.existVideo = true
	}

	s.streamPublisher.Post(&StreamEvent{StreamEventTypeTrack, newTrack})
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
		s.TransDemuxer.DiscardHeadPacket(packet.BufferIndex)
		return
	}

	packetPtr := collections.NewReferenceCounter(packet)
	packetPtr.Refer() // 引用计数加1

	packets := s.originTracks.FindWithType(packet.MediaType).Packets
	packets.Add(packetPtr)
	s.streamPublisher.Post(&StreamEvent{StreamEventTypePacket, packetPtr})

	// 释放未引用的AVPacket
	for packets.Size() > 0 {
		if packets.Get(0).UseCount() > 1 {
			break
		}

		packets.Remove(0).Release()
		s.TransDemuxer.DiscardHeadPacket(packet.BufferIndex)
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

func (s *PublishSource) postEvent(cb func()) {
	s.mainContextEvents <- cb
}

func (s *PublishSource) executeSyncEvent(cb func()) {
	group := sync.WaitGroup{}
	group.Add(1)

	s.postEvent(func() {
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

func (s *PublishSource) GetBitrateStatistics() *BitrateStatistics {
	return s.statistics
}

func (s *PublishSource) ProbeTimeout() {
	if s.TransDemuxer != nil {
		s.TransDemuxer.ProbeComplete()
	}
}

func (s *PublishSource) GetTransStreamPublisher() TransStreamPublisher {
	return s.streamPublisher
}
