package gb28181

type PassiveSource struct {
	BaseGBSource
}

func (p *PassiveSource) SetupType() SetupType {
	return SetupPassive
}

func NewPassiveSource() *PassiveSource {
	return &PassiveSource{}
}
