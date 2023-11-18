package stream

import "github.com/yangjiechina/avformat/utils"

type SinkId string

type ISink interface {
	Id() SinkId

	Input(data []byte)

	Send(buffer utils.ByteBuffer)

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

func AddSinkToWaitingQueue(streamId string, sink ISink) {

}

func RemoveSinkFromWaitingQueue(streamId, sinkId SinkId) ISink {
	return nil
}

func PopWaitingSinks(streamId string) []ISink {
	return nil
}

type SinkImpl struct {
	id          string
	protocol    Protocol
	enableVideo bool

	desiredAudioCodecId utils.AVCodecID
	desiredVideoCodecId utils.AVCodecID
}

func (s *SinkImpl) Id() string {
	return s.id
}

func (s *SinkImpl) Input(data []byte) {
	//TODO implement me
	panic("implement me")
}

func (s *SinkImpl) Send(buffer utils.ByteBuffer) {
	//TODO implement me
	panic("implement me")
}

func (s *SinkImpl) SourceId() string {
	//TODO implement me
	panic("implement me")
}

func (s *SinkImpl) Protocol() Protocol {
	return s.protocol
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
	return s.enableVideo
}

func (s *SinkImpl) SetEnableVideo(enable bool) {
	s.enableVideo = enable
}

func (s *SinkImpl) Close() {
	//TODO implement me
	panic("implement me")
}
