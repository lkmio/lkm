package stream

import (
	"fmt"
	"sync"
)

// SinkManager 目前只用于保存HLS拉流Sink
var SinkManager *sinkManager

func init() {
	SinkManager = &sinkManager{}
}

type sinkManager struct {
	m sync.Map
}

func (s *sinkManager) Add(sink Sink) error {
	_, ok := s.m.LoadOrStore(sink.GetID(), sink)
	if ok {
		return fmt.Errorf("the sink %s has been exist", sink.GetID())
	}

	return nil
}

func (s *sinkManager) Find(id SinkID) Sink {
	value, ok := s.m.Load(id)
	if ok {
		return value.(Sink)
	}

	return nil
}

func (s *sinkManager) Remove(id SinkID) (Sink, error) {
	value, loaded := s.m.LoadAndDelete(id)
	if loaded {
		return value.(Sink), nil
	}

	return nil, fmt.Errorf("source with GetID %s was not find", id)
}

func (s *sinkManager) Exist(id SinkID) bool {
	_, ok := s.m.Load(id)
	return ok
}
