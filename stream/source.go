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
	StreamEndInfoBride func(source string, streams map[TransStreamID]TransStream) *StreamEndInfo
)

// Source 对推流源的封装
type Source interface {
	// GetID 返回SourceID
	GetID() string

	SetID(id string)

	// Input 输入推流数据
	Input(data []byte) (int, error)

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

	Init()

	RemoteAddr() string

	String() string

	State() SessionState

	// UrlValues 返回推流url参数
	UrlValues() url.Values

	// SetUrlValues 设置推流url参数
	SetUrlValues(values url.Values)

	// LastPacketTime 返回最近收流时间戳
	LastPacketTime() time.Time

	SetLastPacketTime(time2 time.Time)

	IsClosed() bool

	CreateTime() time.Time

	SetCreateTime(time time.Time)

	GetBitrateStatistics() *BitrateStatistics

	ProbeTimeout()

	GetTransStreamPublisher() TransStreamPublisher

	StartTimers(source Source)

	ExecuteSyncEvent(cb func())

	UpdateReceiveStats(dataLen int)
}

type PublishSource struct {
	ID    string
	Type  SourceType
	state SessionState
	Conn  net.Conn

	streamPublisher TransStreamPublisher // 解析出来的AVStream和AVPacket, 交由streamPublisher处理

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
	//	streamLock     sync.RWMutex
	streamLock sync.Mutex

	timers struct {
		receiveTimer *time.Timer // 收流超时计时器
		idleTimer    *time.Timer // 空闲超时计时器
		probeTimer   *time.Timer // tack探测超时计时器
	}
}

func (s *PublishSource) SetLastPacketTime(time2 time.Time) {
	s.lastPacketTime = time2
}

func (s *PublishSource) IsClosed() bool {
	return s.closed.Load()
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

func (s *PublishSource) Init() {
	s.SetState(SessionStateHandshakeSuccess)

	s.statistics = NewBitrateStatistics()
	s.streamPublisher = NewTransStreamPublisher(s.ID)
	// 设置探测时长
	s.TransDemuxer.SetProbeDuration(AppConfig.ProbeTimeout)
}

func (s *PublishSource) UpdateReceiveStats(dataLen int) {
	s.statistics.Input(dataLen)
	if AppConfig.ReceiveTimeout > 0 {
		s.SetLastPacketTime(time.Now())
	}
}

func (s *PublishSource) Input(data []byte) (int, error) {
	s.UpdateReceiveStats(len(data))
	var n int
	var err error
	s.ExecuteSyncEvent(func() {
		if s.closed.Load() {
			err = fmt.Errorf("source closed")
		} else {
			n, err = s.TransDemuxer.Input(data)
		}
	})

	return n, err
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

	var closed bool
	s.ExecuteSyncEvent(func() {
		closed = s.closed.Swap(true)
	})

	if closed {
		return
	}

	// 关闭各种超时计时器
	if s.timers.receiveTimer != nil {
		s.timers.receiveTimer.Stop()
	}

	if s.timers.idleTimer != nil {
		s.timers.idleTimer.Stop()
	}

	if s.timers.probeTimer != nil {
		s.timers.probeTimer.Stop()
	}

	// 关闭推流源的解复用器, 不再接收数据
	if s.TransDemuxer != nil {
		s.TransDemuxer.Close()
	}

	// 释放packet
	for _, track := range s.originTracks.All() {
		s.clearUnusedPackets(track.Packets)
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
	s.DoClose()
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
		// 异步执行ProbeTimeout函数中还没释放锁
		go s.DoClose()
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
	s.clearUnusedPackets(packets)
}

func (s *PublishSource) clearUnusedPackets(packets *collections.LinkedList[*collections.ReferenceCounter[*avformat.AVPacket]]) {
	for packets.Size() > 0 {
		if packets.Get(0).UseCount() > 1 {
			break
		}

		unusedPacketPtr := packets.Remove(0)
		bufferIndex := unusedPacketPtr.Get().BufferIndex
		// 引用计数减1
		unusedPacketPtr.Release()
		// AVPacket放回池中, 减少AVPacket分配
		avformat.FreePacket(unusedPacketPtr.Get())
		// 释放AVPacket的Data缓冲区
		s.TransDemuxer.DiscardHeadPacket(bufferIndex)
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

func (s *PublishSource) ExecuteSyncEvent(cb func()) {
	// 无竞争情况下, 接近原子操作
	s.streamLock.Lock()
	defer s.streamLock.Unlock()
	cb()
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
		s.ExecuteSyncEvent(func() {
			if !s.closed.Load() {
				s.TransDemuxer.ProbeComplete()
			}
		})
	}
}

func (s *PublishSource) GetTransStreamPublisher() TransStreamPublisher {
	return s.streamPublisher
}

func (s *PublishSource) StartTimers(source Source) {

	// 开启收流超时计时器
	if AppConfig.ReceiveTimeout > 0 {
		s.timers.receiveTimer = StartReceiveDataTimer(source)
	}

	// 开启拉流空闲超时计时器
	if AppConfig.Hooks.IsEnableOnIdleTimeout() && AppConfig.IdleTimeout > 0 {
		s.timers.idleTimer = StartIdleTimer(source)
	}

	// 开启探测超时计时器
	s.timers.probeTimer = time.AfterFunc(time.Duration(AppConfig.ProbeTimeout)*time.Millisecond, func() {
		if source.IsCompleted() {
			return
		}

		source.ProbeTimeout()
	})

}
