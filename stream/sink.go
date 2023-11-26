package stream

import (
	"github.com/yangjiechina/avformat/utils"
	"net"
)

type SinkId interface{}

type ISink interface {
	Id() SinkId

	Input(data []byte) error

	SourceId() string

	Protocol() Protocol

	State() int

	SetState(state int)

	EnableVideo() bool

	// SetEnableVideo 允许客户端只拉取音频流
	SetEnableVideo(enable bool)

	// DesiredAudioCodecId 允许客户端拉取指定的音频流
	DesiredAudioCodecId() utils.AVCodecID

	// DesiredVideoCodecId DescribeVideoCodecId 允许客户端拉取指定的视频流
	DesiredVideoCodecId() utils.AVCodecID

	Close()
}

// GenerateSinkId 根据Conn生成SinkId IPV4使用一个uint64, IPV6使用String
func GenerateSinkId(conn net.Conn) SinkId {
	network := conn.RemoteAddr().Network()
	if "tcp" == network {
		id := uint64(utils.BytesToInt(conn.RemoteAddr().(*net.TCPAddr).IP.To4()))
		id <<= 32
		id |= uint64(conn.RemoteAddr().(*net.TCPAddr).Port << 16)

		return id
	} else if "udp" == network {
		id := uint64(utils.BytesToInt(conn.RemoteAddr().(*net.UDPAddr).IP.To4()))
		id <<= 32
		id |= uint64(conn.RemoteAddr().(*net.UDPAddr).Port << 16)

		return id
	}

	return conn.RemoteAddr().String()
}

var waitingSinks map[string]map[SinkId]ISink

func init() {
	waitingSinks = make(map[string]map[SinkId]ISink, 1024)
}

func AddSinkToWaitingQueue(streamId string, sink ISink) {
	waitingSinks[streamId][sink.Id()] = sink
}

func RemoveSinkFromWaitingQueue(streamId, sinkId SinkId) ISink {
	return nil
}

func PopWaitingSinks(streamId string) []ISink {
	source, ok := waitingSinks[streamId]
	if !ok {
		return nil
	}

	sinks := make([]ISink, len(source))
	var index = 0
	for _, sink := range source {
		sinks[index] = sink
	}
	return sinks
}

type SinkImpl struct {
	Id_          SinkId
	sourceId     string
	Protocol_    Protocol
	disableVideo bool

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

func (s *SinkImpl) SourceId() string {
	return s.sourceId
}

func (s *SinkImpl) Protocol() Protocol {
	return s.Protocol_
}

func (s *SinkImpl) State() int {
	//TODO implement me
	panic("implement me")
}

func (s *SinkImpl) SetState(state int) {
	//TODO implement me
	panic("implement me")
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

}
