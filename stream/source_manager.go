package stream

import (
	"fmt"
	"sync"
)

type ISourceManager interface {
	Add(source ISource) error

	Find(id string) ISource

	Remove(id string) (ISource, error)
}

var SourceManager ISourceManager

func init() {
	SourceManager = &sourceMangerImpl{}
}

type sourceMangerImpl struct {
	m sync.Map
}

func (s *sourceMangerImpl) Add(source ISource) error {
	_, ok := s.m.LoadOrStore(source.Id(), source)
	if ok {
		return fmt.Errorf("the source %s has been exist", source.Id())
	}

	return nil
}

func (s *sourceMangerImpl) Find(id string) ISource {
	value, ok := s.m.Load(id)
	if ok {
		return value.(ISource)
	}

	return nil
}

func (s *sourceMangerImpl) Remove(id string) (ISource, error) {
	value, loaded := s.m.LoadAndDelete(id)
	if loaded {
		return value.(ISource), nil
	}

	return nil, fmt.Errorf("source with id %s was not find", id)
}
