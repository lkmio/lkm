package gb28181

import "github.com/lkmio/transport"

type PassiveSource struct {
	BaseGBSource
	decoder *transport.LengthFieldFrameDecoder
}

// Input 重写stream.Source的Input函数, 主协程把推流数据交给PassiveSource处理
func (p *PassiveSource) Input(data []byte) error {
	_, err := DecodeGBRTPOverTCPPacket(data, p, p.decoder, nil, p.Conn)
	return err
}

func (p *PassiveSource) SetupType() SetupType {
	return SetupPassive
}

func NewPassiveSource() *PassiveSource {
	return &PassiveSource{
		decoder: transport.NewLengthFieldFrameDecoder(0xFFFF, 2),
	}
}
