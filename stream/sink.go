package stream

import (
	"fmt"
	"github.com/lkmio/avformat/utils"
	"net"
	"net/url"
	"sync"
)

// Sink 对拉流端的封装
type Sink interface {
	GetID() SinkID

	SetID(sink SinkID)

	GetSourceID() string

	Input(data []byte) error

	SendHeader(data []byte) error

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

	Start()

	Flush()

	GetConn() net.Conn
}

type BaseSink struct {
	ID            SinkID
	SourceID      string
	Protocol      TransStreamProtocol
	State         SessionState
	TransStreamID TransStreamID
	disableVideo  bool

	lock            sync.RWMutex
	HasSentKeyVideo bool // 是否已经发送视频关键帧，未开启GOP缓存的情况下，为避免播放花屏，发送的首个视频帧必须为关键帧

	DesiredAudioCodecId_ utils.AVCodecID
	DesiredVideoCodecId_ utils.AVCodecID

	Conn      net.Conn
	urlValues url.Values // 拉流时携带的Url参数
}

func (s *BaseSink) GetID() SinkID {
	return s.ID
}

func (s *BaseSink) SetID(id SinkID) {
	s.ID = id
}

func (s *BaseSink) Input(data []byte) error {
	if s.Conn != nil {
		_, err := s.Conn.Write(data)

		return err
	}

	return nil
}

func (s *BaseSink) SendHeader(data []byte) error {
	return s.Input(data)
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
	utils.Assert(!s.lock.TryLock())

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
// 1. Sink如果正在拉流,删除任务交给Source处理. 否则直接从等待队列删除Sink.
// 2. 发送PlayDoneHook事件
// 什么时候调用Close? 是否考虑线程安全?
// 拉流断开连接,不需要考虑线程安全
// 踢流走source管道删除,并且关闭Conn
func (s *BaseSink) Close() {
	if SessionStateClosed == s.State {
		return
	}

	if s.Conn != nil {
		s.Conn.Close()
		s.Conn = nil
	}

	// Sink未添加到任何队列, 不做处理
	if s.State < SessionStateWait {
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
		// 从Source中删除Sink
		source := SourceManager.Find(s.SourceID)
		source.RemoveSink(s)
	} else if state == SessionStateWait {
		// 从等待队列中删除Sink
		RemoveSinkFromWaitingQueue(s.SourceID, s.ID)
		go HookPlayDoneEvent(s)
	}
}

func (s *BaseSink) String() string {
	return fmt.Sprintf("%s-%v source:%s", s.GetProtocol().ToString(), s.ID, s.SourceID)
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

func (s *BaseSink) Start() {

}

func (s *BaseSink) Flush() {

}

func (s *BaseSink) GetConn() net.Conn {
	return s.Conn
}
