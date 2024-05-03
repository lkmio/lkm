package gb28181

import (
	"github.com/pion/rtp"
	"github.com/yangjiechina/live-server/stream"
)

type PassiveSource struct {
	GBSourceImpl
}

func NewPassiveSource() *PassiveSource {
	return &PassiveSource{}
}

func (t PassiveSource) Transport() Transport {
	return TransportTCPPassive
}

func (t PassiveSource) InputRtp(pkt *rtp.Packet) error {
	t.SourceImpl.AddEvent(stream.SourceEventInput, pkt.Payload)
	return nil
}
