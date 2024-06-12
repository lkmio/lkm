package gb28181

import (
	"github.com/yangjiechina/lkm/stream"
	"net"
)

type SingleFilter struct {
	BaseFilter

	source GBSource
}

func NewSingleFilter(source GBSource) *SingleFilter {
	return &SingleFilter{source: source}
}

func (s *SingleFilter) AddSource(ssrc uint32, source GBSource) bool {
	panic("implement me")
}

func (s *SingleFilter) RemoveSource(ssrc uint32) {
	panic("implement me")
}

func (s *SingleFilter) Input(conn net.Conn, data []byte) GBSource {
	packet, err := s.ParseRtpPacket(conn, data)
	if err != nil {
		return nil
	}

	if s.source == nil {
		return nil
	}

	if stream.SessionStateHandshakeDone == s.source.State() {
		s.PreparePublishSource(conn, packet.SSRC, s.source)
	}

	s.source.InputRtp(packet)
	return s.source
}
