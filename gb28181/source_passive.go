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

// InputRtp tcp收流,直接解析ps流.
func (t PassiveSource) InputRtp(pkt *rtp.Packet) error {
	t.Input(pkt.Payload)
	return nil
}
