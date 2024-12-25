package gb28181

type PassiveSource struct {
	BaseGBSource
}

func (t PassiveSource) SetupType() SetupType {
	return SetupPassive
}

func NewPassiveSource() *PassiveSource {
	return &PassiveSource{}
}
