package stream

import (
	"encoding/binary"
	"fmt"
	"github.com/yangjiechina/avformat/utils"
	"net"
	"sync"
)

type SinkId interface{}

type ISink interface {
	Id() SinkId

	Input(data []byte) error

	SendHeader(data []byte) error

	SourceId() string

	TransStreamId() TransStreamId

	SetTransStreamId(id TransStreamId)

	Protocol() Protocol

	ProtocolStr() string

	// State 获取Sink状态, 调用前外部必须手动加锁
	State() SessionState

	// SetState 设置Sink状态, 调用前外部必须手动加锁
	SetState(state SessionState)

	EnableVideo() bool

	// SetEnableVideo 允许客户端只拉取音频流
	SetEnableVideo(enable bool)

	// DesiredAudioCodecId 允许客户端拉取指定的音频流
	DesiredAudioCodecId() utils.AVCodecID

	// DesiredVideoCodecId DescribeVideoCodecId 允许客户端拉取指定的视频流
	DesiredVideoCodecId() utils.AVCodecID

	// Close 关闭释放Sink, 从传输流或等待队列中删除sink
	Close()

	PrintInfo() string

	// Lock Sink请求拉流->Source推流->Sink断开整个阶段, 是无锁线程安全
	//如果Sink在等待队列-Sink断开, 这个过程是非线程安全的
	//所以Source在AddSink时, SessionStateWait状态时, 需要加锁保护.
	Lock()

	UnLock()
}

// GenerateSinkId 根据网络地址生成SinkId IPV4使用一个uint64, IPV6使用String
func GenerateSinkId(addr net.Addr) SinkId {
	network := addr.Network()
	if "tcp" == network {
		id := uint64(binary.BigEndian.Uint32(addr.(*net.TCPAddr).IP.To4()))
		id <<= 32
		id |= uint64(addr.(*net.TCPAddr).Port << 16)

		return id
	} else if "udp" == network {
		id := uint64(binary.BigEndian.Uint32(addr.(*net.UDPAddr).IP.To4()))
		id <<= 32
		id |= uint64(addr.(*net.UDPAddr).Port << 16)

		return id
	}

	return addr.String()
}

type SinkImpl struct {
	hookSessionImpl

	Id_            SinkId
	SourceId_      string
	Protocol_      Protocol
	State_         SessionState
	TransStreamId_ TransStreamId
	disableVideo   bool

	lock sync.RWMutex

	//HasSentKeyVideo 是否已经发送视频关键帧
	//未开启GOP缓存的情况下，为避免播放花屏，发送的首个视频帧必须为关键帧
	HasSentKeyVideo bool

	DesiredAudioCodecId_ utils.AVCodecID
	DesiredVideoCodecId_ utils.AVCodecID

	Conn net.Conn
}

func (s *SinkImpl) Id() SinkId {
	return s.Id_
}

func (s *SinkImpl) Input(data []byte) error {
	if s.Conn != nil {
		_, err := s.Conn.Write(data)

		return err
	}

	return nil
}

func (s *SinkImpl) SendHeader(data []byte) error {
	return s.Input(data)
}

func (s *SinkImpl) SourceId() string {
	return s.SourceId_
}

func (s *SinkImpl) TransStreamId() TransStreamId {
	return s.TransStreamId_
}

func (s *SinkImpl) SetTransStreamId(id TransStreamId) {
	s.TransStreamId_ = id
}

func (s *SinkImpl) Protocol() Protocol {
	return s.Protocol_
}

func (s *SinkImpl) ProtocolStr() string {
	return streamTypeToStr(s.Protocol_)
}

func (s *SinkImpl) Lock() {
	s.lock.Lock()
}

func (s *SinkImpl) UnLock() {
	s.lock.Unlock()
}

func (s *SinkImpl) State() SessionState {
	utils.Assert(!s.lock.TryLock())

	return s.State_
}

func (s *SinkImpl) SetState(state SessionState) {
	utils.Assert(!s.lock.TryLock())

	s.State_ = state
}

func (s *SinkImpl) EnableVideo() bool {
	return !s.disableVideo
}

func (s *SinkImpl) SetEnableVideo(enable bool) {
	s.disableVideo = !enable
}

func (s *SinkImpl) DesiredAudioCodecId() utils.AVCodecID {
	return s.DesiredAudioCodecId_
}

func (s *SinkImpl) DesiredVideoCodecId() utils.AVCodecID {
	return s.DesiredVideoCodecId_
}

// Close 做如下事情:
// 1. Sink如果正在拉流,删除任务交给Source处理. 否则直接从等待队列删除Sink.
// 2. 发送PlayDoneHook事件
// 什么时候调用Close? 是否考虑线程安全?
// 拉流断开连接,不需要考虑线程安全
// 踢流走source管道删除,并且关闭Conn
func (s *SinkImpl) Close() {
	utils.Assert(SessionStateClose != s.State_)

	if s.Conn != nil {
		s.Conn.Close()
		s.Conn = nil
	}

	//还没有添加到任何队列, 不做任何处理
	if s.State_ < SessionStateWait {
		return
	}

	{
		s.Lock()
		defer s.UnLock()
		if s.State_ == SessionStateClose {
			return
		}

		s.State_ = SessionStateClose
	}

	if s.State_ == SessionStateTransferring {
		source := SourceManager.Find(s.SourceId_)
		source.AddEvent(SourceEventPlayDone, s)
	} else if s.State_ == SessionStateWait {
		RemoveSinkFromWaitingQueue(s.SourceId_, s.Id_)
		//拉流结束事件, 在等待队列直接发送通知, 在拉流由Source负责发送.
		HookPlayingDone(s, nil, nil)
	}
}

func (s *SinkImpl) PrintInfo() string {
	return fmt.Sprintf("%s-%v source:%s", s.ProtocolStr(), s.Id_, s.SourceId_)
}
