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
	//TCP收流, 解析rtp后直接送给ps解析
	t.Input(pkt.Payload)
	return nil
}
