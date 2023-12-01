package stream

import "sync"

var waitingSinks map[string]map[SinkId]ISink

var mutex sync.Mutex

func init() {
	waitingSinks = make(map[string]map[SinkId]ISink, 1024)
}

func AddSinkToWaitingQueue(streamId string, sink ISink) {
	m, ok := waitingSinks[streamId]
	if !ok {
		mutex.Lock()
		mutex.Unlock()
		if m, ok = waitingSinks[streamId]; !ok {
			m = make(map[SinkId]ISink, 64)
			waitingSinks[streamId] = m
		}
	}

	m[sink.Id()] = sink
}

func RemoveSinkFromWaitingQueue(sourceId string, sinkId SinkId) (ISink, bool) {
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
	source, ok := waitingSinks[sourceId]
	if !ok {
		return nil
	}

	sinks := make([]ISink, len(source))
	var index = 0
	for _, sink := range source {
		sinks[index] = sink
	}
	return sinks
}
