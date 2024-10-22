package stream

import "sync"

// 等待队列所有的Sink
var waitingSinks map[string]map[SinkID]Sink

var mutex sync.RWMutex

func init() {
	waitingSinks = make(map[string]map[SinkID]Sink, 1024)
}

func AddSinkToWaitingQueue(streamId string, sink Sink) {
	mutex.Lock()
	defer mutex.Unlock()

	m, ok := waitingSinks[streamId]
	if !ok {
		if m, ok = waitingSinks[streamId]; !ok {
			m = make(map[SinkID]Sink, 64)
			waitingSinks[streamId] = m
		}
	}

	m[sink.GetID()] = sink
}

func RemoveSinkFromWaitingQueue(sourceId string, sinkId SinkID) (Sink, bool) {
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

func PopWaitingSinks(sourceId string) []Sink {
	mutex.Lock()
	defer mutex.Unlock()

	source, ok := waitingSinks[sourceId]
	if !ok {
		return nil
	}

	sinks := make([]Sink, len(source))
	var index = 0
	for _, sink := range source {
		sinks[index] = sink
		index++
	}

	delete(waitingSinks, sourceId)
	return sinks
}

func ExistSinkInWaitingQueue(sourceId string, sinkId SinkID) bool {
	mutex.RLock()
	defer mutex.RUnlock()

	source, ok := waitingSinks[sourceId]
	if !ok {
		return false
	}

	_, ok = source[sinkId]
	return ok
}

func ExistSink(sourceId string, sinkId SinkID) bool {
	if sourceId != "" {
		if exist := ExistSinkInWaitingQueue(sourceId, sinkId); exist {
			return true
		}
	}

	return SinkManager.Exist(sinkId)
}
