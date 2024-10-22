package gb28181

type singleFilter struct {
	source GBSource
}

func (s *singleFilter) AddSource(ssrc uint32, source GBSource) bool {
	panic("implement me")
}

func (s *singleFilter) RemoveSource(ssrc uint32) {
	s.source = nil
}

func (s *singleFilter) FindSource(ssrc uint32) GBSource {
	return s.source
}

func NewSingleFilter(source GBSource) Filter {
	return &singleFilter{source: source}
}
