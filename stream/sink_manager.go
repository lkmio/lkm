package stream

import (
	"fmt"
	"sync"
)

// 等待队列所有的Sink
var waitingSinks map[string]map[SinkId]ISink

var mutex sync.RWMutex

func init() {
	waitingSinks = make(map[string]map[SinkId]ISink, 1024)
}

func AddSinkToWaitingQueue(streamId string, sink ISink) {
	mutex.Lock()
	defer mutex.Unlock()

	m, ok := waitingSinks[streamId]
	if !ok {
		if m, ok = waitingSinks[streamId]; !ok {
			m = make(map[SinkId]ISink, 64)
			waitingSinks[streamId] = m
		}
	}

	m[sink.Id()] = sink
}

func RemoveSinkFromWaitingQueue(sourceId string, sinkId SinkId) (ISink, bool) {
	mutex.Lock()
	defer mutex.Unlock()

	m, ok := waitingSinks[sourceId]
	if !ok {
		return nil, false
	}

	sink, ok := m[sinkId]
	if ok {
		delete(m, sinkId)
	}

	return sink, ok
}

func PopWaitingSinks(sourceId string) []ISink {
	mutex.Lock()
	defer mutex.Unlock()

	source, ok := waitingSinks[sourceId]
	if !ok {
		return nil
	}

	sinks := make([]ISink, len(source))
	var index = 0
	for _, sink := range source {
		sinks[index] = sink
		index++
	}

	delete(waitingSinks, sourceId)
	return sinks
}

func ExistSinkInWaitingQueue(sourceId string, sinkId SinkId) bool {
	mutex.RLock()
	defer mutex.RUnlock()

	source, ok := waitingSinks[sourceId]
	if !ok {
		return false
	}

	_, ok = source[sinkId]
	return ok
}

func ExistSink(sourceId string, sinkId SinkId) bool {
	if sourceId != "" {
		if exist := ExistSinkInWaitingQueue(sourceId, sinkId); exist {
			return true
		}
	}

	return SinkManager.Exist(sinkId)
}

// ISinkManager 添加到TransStream的所有Sink
type ISinkManager interface {
	Add(sink ISink) error

	Find(id SinkId) ISink

	Remove(id SinkId) (ISink, error)

	Exist(id SinkId) bool
}

var SinkManager ISinkManager

func init() {
	SinkManager = &sinkManagerImpl{}
}

type sinkManagerImpl struct {
	m sync.Map
}

func (s *sinkManagerImpl) Add(sink ISink) error {
	_, ok := s.m.LoadOrStore(sink.Id(), sink)
	if ok {
		return fmt.Errorf("the sink %s has been exist", sink.Id())
	}

	return nil
}

func (s *sinkManagerImpl) Find(id SinkId) ISink {
	value, ok := s.m.Load(id)
	if ok {
		return value.(ISink)
	}

	return nil
}

func (s *sinkManagerImpl) Remove(id SinkId) (ISink, error) {
	value, loaded := s.m.LoadAndDelete(id)
	if loaded {
		return value.(ISink), nil
	}

	return nil, fmt.Errorf("source with id %s was not find", id)
}

func (s *sinkManagerImpl) Exist(id SinkId) bool {
	_, ok := s.m.Load(id)
	return ok
}
