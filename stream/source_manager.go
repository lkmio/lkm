package stream

import (
	"fmt"
	"sync"
)

// SourceManager 全局管理所有推流源
var SourceManager *sourceManger

func init() {
	SourceManager = &sourceManger{}
}

type sourceManger struct {
	m sync.Map
}

func (s *sourceManger) Add(source Source) error {
	_, ok := s.m.LoadOrStore(source.GetID(), source)
	if ok {
		return fmt.Errorf("the source %s has been exist", source.GetID())
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

	return nil, fmt.Errorf("source with GetID %s was not find", id)
}

func (s *sourceManger) All() []Source {
	var all []Source

	s.m.Range(func(key, value any) bool {
		all = append(all, value.(Source))
		return true
	})

	return all
}
