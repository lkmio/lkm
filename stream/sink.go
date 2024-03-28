package stream

import (
	"fmt"
	"github.com/yangjiechina/avformat/utils"
	"net"
	"net/http"
)

type SinkId interface{}

type ISink interface {
	HookHandler

	Id() SinkId

	Input(data []byte) error

	SendHeader(data []byte) error

	SourceId() string

	TransStreamId() TransStreamId

	SetTransStreamId(id TransStreamId)

	Protocol() Protocol

	State() SessionState

	SetState(state SessionState) bool

	EnableVideo() bool

	// SetEnableVideo 允许客户端只拉取音频流
	SetEnableVideo(enable bool)

	// DesiredAudioCodecId 允许客户端拉取指定的音频流
	DesiredAudioCodecId() utils.AVCodecID

	// DesiredVideoCodecId DescribeVideoCodecId 允许客户端拉取指定的视频流
	DesiredVideoCodecId() utils.AVCodecID

	Close()
}

// GenerateSinkId 根据网络地址生成SinkId IPV4使用一个uint64, IPV6使用String
func GenerateSinkId(addr net.Addr) SinkId {
	network := addr.Network()
	if "tcp" == network {
		id := uint64(utils.BytesToInt(addr.(*net.TCPAddr).IP.To4()))
		id <<= 32
		id |= uint64(addr.(*net.TCPAddr).Port << 16)

		return id
	} else if "udp" == network {
		id := uint64(utils.BytesToInt(addr.(*net.UDPAddr).IP.To4()))
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

	//Sink在请求拉流->Source推流->Sink断开整个阶段 是无锁线程安全
	//如果Sink在等待队列-Sink断开，这个过程是非线程安全的
	//SetState的时候，如果closed为true，返回false, 调用者自行删除sink
	//closed atomic.Bool

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

func (s *SinkImpl) State() SessionState {
	return s.State_
}

func (s *SinkImpl) SetState(state SessionState) bool {
	//load := s.closed.Load()
	//if load {
	//	return false
	//}

	if s.State_ < SessionStateClose {
		s.State_ = state
	}

	//更改状态期间，被Close
	//if s.closed.CompareAndSwap(false, false)
	//{
	//
	//}

	//return !s.closed.Load()
	return true
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

func (s *SinkImpl) Close() {
	//Source的TransStream中删除sink
	if s.State_ == SessionStateTransferring {
		source := SourceManager.Find(s.SourceId_)
		source.AddEvent(SourceEventPlayDone, s)
		s.State_ = SessionStateClose
	} else if s.State_ == SessionStateWait {
		//非线程安全
		//从等待队列中删除sink
		RemoveSinkFromWaitingQueue(s.SourceId_, s.Id_)
		s.State_ = SessionStateClose
		//s.closed.Store(true)
	}
}

func (s *SinkImpl) Play(sink ISink, success func(), failure func(state utils.HookState)) {
	f := func() {
		source := SourceManager.Find(sink.SourceId())
		if source == nil {
			fmt.Printf("添加到等待队列 sink:%s", sink.Id())
			sink.SetState(SessionStateWait)
			AddSinkToWaitingQueue(sink.SourceId(), sink)
		} else {
			source.AddEvent(SourceEventPlay, sink)
		}
	}

	if !AppConfig.Hook.EnableOnPlay() {
		f()
		success()
		return
	}

	err := s.Hook(HookEventPlay, NewPlayHookEventInfo(sink.SourceId(), "", sink.Protocol()), func(response *http.Response) {
		f()
		success()
	}, func(response *http.Response, err error) {
		failure(utils.HookStateFailure)
	})

	if err != nil {
		failure(utils.HookStateFailure)
		return
	}
}

func (s *SinkImpl) PlayDone(source ISink, success func(), failure func(state utils.HookState)) {

}
