package stream

import "github.com/yangjiechina/avformat/utils"

type ISink interface {
	Id() string

	Input(data []byte)

	Send(buffer utils.ByteBuffer)

	SourceId() string

	Protocol() int

	State() int

	SetState(state int)

	DisableVideo() bool

	SetEnableVideo(enable bool)

	Close()
}

func AddSinkToWaitingQueue(streamId string, sink ISink) {

}

func RemoveSinkFromWaitingQueue(streamId, sinkId string) ISink {
	return nil
}

func PopWaitingSinks(streamId string) []ISink {
	return nil
}

type SinkImpl struct {
}

func (s *SinkImpl) Id() string {
	//TODO implement me
	panic("implement me")
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

func (s *SinkImpl) Protocol() int {
	//TODO implement me
	panic("implement me")
}

func (s *SinkImpl) State() int {
	//TODO implement me
	panic("implement me")
}

func (s *SinkImpl) SetState(state int) {
	//TODO implement me
	panic("implement me")
}

func (s *SinkImpl) DisableVideo() bool {
	//TODO implement me
	panic("implement me")
}

func (s *SinkImpl) SetEnableVideo(enable bool) {
	//TODO implement me
	panic("implement me")
}

func (s *SinkImpl) Close() {
	//TODO implement me
	panic("implement me")
}

