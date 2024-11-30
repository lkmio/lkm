package stream

import (
	"fmt"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/log"
	"net"
	"net/url"
	"sync"
	"time"
)

// Sink 对拉流端的封装
type Sink interface {
	GetID() SinkID

	SetID(sink SinkID)

	GetSourceID() string

	Write(index int, data [][]byte, ts int64) error

	GetTransStreamID() TransStreamID

	SetTransStreamID(id TransStreamID)

	GetProtocol() TransStreamProtocol

	// GetState 获取Sink状态, 调用前外部必须手动加锁
	GetState() SessionState

	// SetState 设置Sink状态, 调用前外部必须手动加锁
	SetState(state SessionState)

	EnableVideo() bool

	// SetEnableVideo 设置是否拉取视频流, 允许客户端只拉取音频流
	SetEnableVideo(enable bool)

	// DesiredAudioCodecId 允许客户端拉取指定的音频流
	DesiredAudioCodecId() utils.AVCodecID

	// DesiredVideoCodecId DescribeVideoCodecId 允许客户端拉取指定的视频流
	DesiredVideoCodecId() utils.AVCodecID

	// Close 关闭释放Sink, 从传输流或等待队列中删除sink
	Close()

	String() string

	RemoteAddr() string

	// Lock Sink请求拉流->Source推流->Sink断开整个阶段, 是无锁线程安全
	// 如果Sink在等待队列-Sink断开, 这个过程是非线程安全的
	// 所以Source在AddSink时, SessionStateWait状态时, 需要加锁保护.
	Lock()

	UnLock()

	UrlValues() url.Values

	SetUrlValues(values url.Values)

	// StartStreaming Source向Sink开始推流时调用
	StartStreaming(stream TransStream) error

	// StopStreaming Source向Sink停止推流时调用
	StopStreaming(stream TransStream)

	GetConn() net.Conn

	IsTCPStreaming() bool

	GetSentPacketCount() int

	SetSentPacketCount(int)

	IncreaseSentPacketCount()

	IsReady() bool

	SetReady(ok bool)

	CreateTime() time.Time

	SetCreateTime(time time.Time)
}

type BaseSink struct {
	ID            SinkID
	SourceID      string
	Protocol      TransStreamProtocol
	State         SessionState
	TransStreamID TransStreamID
	disableVideo  bool

	lock sync.RWMutex

	DesiredAudioCodecId_ utils.AVCodecID
	DesiredVideoCodecId_ utils.AVCodecID

	Conn         net.Conn   // 拉流信令链路
	TCPStreaming bool       // 是否是TCP流式拉流
	urlValues    url.Values // 拉流时携带的Url参数

	SentPacketCount int  // 发包计数
	Ready           bool // 是否准备好推流. Sink可以通过控制该变量, 达到触发Source推流, 但不立即拉流的目的. 比如rtsp拉流端在信令交互阶段,需要先获取媒体信息,再拉流.
	createTime      time.Time
}

func (s *BaseSink) GetID() SinkID {
	return s.ID
}

func (s *BaseSink) SetID(id SinkID) {
	s.ID = id
}

func (s *BaseSink) Write(index int, data [][]byte, ts int64) error {
	if s.Conn != nil {
		for _, bytes := range data {
			_, err := s.Conn.Write(bytes)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *BaseSink) GetSourceID() string {
	return s.SourceID
}

func (s *BaseSink) GetTransStreamID() TransStreamID {
	return s.TransStreamID
}

func (s *BaseSink) SetTransStreamID(id TransStreamID) {
	s.TransStreamID = id
}

func (s *BaseSink) GetProtocol() TransStreamProtocol {
	return s.Protocol
}

func (s *BaseSink) Lock() {
	s.lock.Lock()
}

func (s *BaseSink) UnLock() {
	s.lock.Unlock()
}

func (s *BaseSink) GetState() SessionState {
	//utils.Assert(!s.lock.TryLock())

	return s.State
}

func (s *BaseSink) SetState(state SessionState) {
	utils.Assert(!s.lock.TryLock())

	s.State = state
}

func (s *BaseSink) EnableVideo() bool {
	return !s.disableVideo
}

func (s *BaseSink) SetEnableVideo(enable bool) {
	s.disableVideo = !enable
}

func (s *BaseSink) DesiredAudioCodecId() utils.AVCodecID {
	return s.DesiredAudioCodecId_
}

func (s *BaseSink) DesiredVideoCodecId() utils.AVCodecID {
	return s.DesiredVideoCodecId_
}

// Close 做如下事情:
// 1. Sink如果正在拉流, 删除任务交给Source处理, 否则直接从等待队列删除Sink.
// 2. 发送PlayDoneHook事件
func (s *BaseSink) Close() {
	log.Sugar.Debugf("closing the %s sink. id: %s. current session state: %s", s.Protocol, SinkId2String(s.ID), s.State)

	if SessionStateClosed == s.State {
		return
	}

	if s.Conn != nil {
		s.Conn.Close()
		s.Conn = nil
	}

	// Sink未添加到任何队列, 不做处理
	if s.State < SessionStateWaiting {
		return
	}

	// 更新Sink状态
	var state SessionState
	{
		s.Lock()
		defer s.UnLock()
		if s.State == SessionStateClosed {
			return
		}

		state = s.State
		s.State = SessionStateClosed
	}

	if state == SessionStateTransferring {
		// 从source中删除sink, 如果source为nil, 已经结束推流.
		if source := SourceManager.Find(s.SourceID); source != nil {
			source.RemoveSink(s)
		}
	} else if state == SessionStateWaiting {
		// 从等待队列中删除Sink
		RemoveSinkFromWaitingQueue(s.SourceID, s.ID)
		go HookPlayDoneEvent(s)

		// 等待队列为空, 不再保留推流源信息
		if !ExistSourceInWaitingQueue(s.SourceID) {
			streamEndInfoManager.Remove(s.SourceID)
		}
	}
}

func (s *BaseSink) String() string {
	return fmt.Sprintf("%s-%v source:%s", s.GetProtocol().String(), s.ID, s.SourceID)
}

func (s *BaseSink) RemoteAddr() string {
	if s.Conn == nil {
		return ""
	}

	return s.Conn.RemoteAddr().String()
}

func (s *BaseSink) UrlValues() url.Values {
	return s.urlValues
}

func (s *BaseSink) SetUrlValues(values url.Values) {
	s.urlValues = values
}

func (s *BaseSink) StartStreaming(stream TransStream) error {
	return nil
}

func (s *BaseSink) StopStreaming(stream TransStream) {
	s.SentPacketCount = 0
}

func (s *BaseSink) GetConn() net.Conn {
	return s.Conn
}

func (s *BaseSink) IsTCPStreaming() bool {
	return s.TCPStreaming
}

func (s *BaseSink) GetSentPacketCount() int {
	return s.SentPacketCount
}

func (s *BaseSink) SetSentPacketCount(count int) {
	s.SentPacketCount = count
}

func (s *BaseSink) IncreaseSentPacketCount() {
	s.SentPacketCount++
}

func (s *BaseSink) IsReady() bool {
	return s.Ready
}

func (s *BaseSink) SetReady(ok bool) {
	s.Ready = ok
	if ok {
		s.SetCreateTime(time.Now())
	}
}

func (s *BaseSink) CreateTime() time.Time {
	return s.createTime
}

func (s *BaseSink) SetCreateTime(time time.Time) {
	s.createTime = time
}
