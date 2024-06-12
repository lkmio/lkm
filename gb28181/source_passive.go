package gb28181

import (
	"github.com/pion/rtp"
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
	t.PublishSource.Input(pkt.Payload)
	return nil
}
