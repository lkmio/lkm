package stream

import (
	"fmt"
	"sync"
)

var SourceManager *sourceManger

func init() {
	SourceManager = &sourceManger{}
}

type sourceManger struct {
	m sync.Map
}

func (s *sourceManger) Add(source Source) error {
	_, ok := s.m.LoadOrStore(source.Id(), source)
	if ok {
		return fmt.Errorf("the source %s has been exist", source.Id())
	}

	return nil
}

func (s *sourceManger) Find(id string) Source {
	value, ok := s.m.Load(id)
	if ok {
		return value.(Source)
	}

	return nil
}

func (s *sourceManger) Remove(id string) (Source, error) {
	value, loaded := s.m.LoadAndDelete(id)
	if loaded {
		return value.(Source), nil
	}

	return nil, fmt.Errorf("source with id %s was not find", id)
}
