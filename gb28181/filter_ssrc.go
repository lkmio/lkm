package gb28181

import (
	"net"
)

type SSRCFilter struct {
	FilterImpl

	sources map[uint32]GBSource
}

func NewSharedFilter(guestCount int) *SSRCFilter {
	return &SSRCFilter{sources: make(map[uint32]GBSource, guestCount)}
}

func (r SSRCFilter) AddSource(ssrc uint32, source GBSource) bool {
	_, ok := r.sources[ssrc]
	if ok {
		return false
	}

	r.sources[ssrc] = source
	return true
}

func (r SSRCFilter) Input(conn net.Conn, data []byte) GBSource {
	packet, err := r.ParseRtpPacket(conn, data)
	if err != nil {
		return nil
	}

	source, ok := r.sources[packet.SSRC]
	if !ok {
		return nil
	}

	source.InputRtp(packet)
	return source
}
