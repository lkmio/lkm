package stream

import (
	"fmt"
	"sync"
)

var SinkManager *sinkManager

func init() {
	SinkManager = &sinkManager{}
}

// ISinkManager 添加到TransStream的所有Sink
type sinkManager struct {
	m sync.Map
}

func (s *sinkManager) Add(sink Sink) error {
	_, ok := s.m.LoadOrStore(sink.Id(), sink)
	if ok {
		return fmt.Errorf("the sink %s has been exist", sink.Id())
	}

	return nil
}

func (s *sinkManager) Find(id SinkId) Sink {
	value, ok := s.m.Load(id)
	if ok {
		return value.(Sink)
	}

	return nil
}

func (s *sinkManager) Remove(id SinkId) (Sink, error) {
	value, loaded := s.m.LoadAndDelete(id)
	if loaded {
		return value.(Sink), nil
	}

	return nil, fmt.Errorf("source with id %s was not find", id)
}

func (s *sinkManager) Exist(id SinkId) bool {
	_, ok := s.m.Load(id)
	return ok
}
