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

func (t PassiveSource) SetupType() SetupType {
	return SetupPassive
}

func (t PassiveSource) InputRtpPacket(pkt *rtp.Packet) error {
	return t.Input(pkt.Payload)
}
