package gb28181

import (
	"github.com/pion/rtp"
	"github.com/yangjiechina/lkm/stream"
)

type PassiveSource struct {
	BaseGBSource
}

func NewPassiveSource() *PassiveSource {
	return &PassiveSource{}
}

func (t PassiveSource) TransportType() TransportType {
	return TransportTypeTCPPassive
}

func (t PassiveSource) InputRtp(pkt *rtp.Packet) error {
	t.PublishSource.AddEvent(stream.SourceEventInput, pkt.Payload)
	return nil
}
