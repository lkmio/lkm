package gb28181

import "github.com/lkmio/avformat/transport"

type PassiveSource struct {
	BaseGBSource
	decoder *transport.LengthFieldFrameDecoder
}

// Input 重写stream.Source的Input函数, 主解析将会把推流数据交给PassiveSource处理
func (p PassiveSource) Input(data []byte) error {
	return p.decoder.Input(data)
}

func (p PassiveSource) SetupType() SetupType {
	return SetupPassive
}

func NewPassiveSource() *PassiveSource {
	return &PassiveSource{}
}
