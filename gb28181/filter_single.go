package gb28181

import (
	"net"
)

type SingleFilter struct {
	FilterImpl

	source GBSource
}

func NewSingleFilter(source GBSource) *SingleFilter {
	return &SingleFilter{source: source}
}

func (s *SingleFilter) AddSource(ssrc uint32, source GBSource) bool {
	panic("implement me")
	/*	utils.Assert(s.source == nil)
		s.source = source
		return true*/
}

func (s *SingleFilter) Input(conn net.Conn, data []byte) GBSource {
	packet, err := s.ParseRtpPacket(conn, data)
	if err != nil {
		return nil
	}

	if s.source == nil {
		return nil
	}

	s.source.InputRtp(packet)
	return s.source
}
