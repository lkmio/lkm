package gb28181

type singleFilter struct {
	source GBSource
}

func NewSingleFilter(source GBSource) Filter {
	return &singleFilter{source: source}
}

func (s *singleFilter) AddSource(ssrc uint32, source GBSource) bool {
	panic("implement me")
}

func (s *singleFilter) RemoveSource(ssrc uint32) {
	panic("implement me")
}

func (s *singleFilter) FindSource(ssrc uint32) GBSource {
	return s.source
}
