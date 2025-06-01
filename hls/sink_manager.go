package hls

import (
	"github.com/lkmio/lkm/log"
	"github.com/lkmio/lkm/stream"
	"sync"
	"time"
)

// SinkManager 目前只用于保存HLS拉流Sink
var SinkManager *sinkManager

func init() {
	SinkManager = &sinkManager{}
}

type sinkManager struct {
	m sync.Map
}

func (s *sinkManager) Add(sink stream.Sink) bool {
	_, ok := s.m.LoadOrStore(sink.GetID(), sink)
	return !ok
}

func (s *sinkManager) Find(id stream.SinkID) stream.Sink {
	value, ok := s.m.Load(id)
	if ok {
		return value.(stream.Sink)
	}

	return nil
}

func (s *sinkManager) Remove(id stream.SinkID) stream.Sink {
	value, loaded := s.m.LoadAndDelete(id)
	if loaded {
		return value.(stream.Sink)
	}

	return nil
}

func (s *sinkManager) Exist(id stream.SinkID) bool {
	_, ok := s.m.Load(id)
	return ok
}

// StartPullStreamTimeoutTimer 开启hls拉流计时器, 用于检测hls拉流超时
func (s *sinkManager) StartPullStreamTimeoutTimer(interval, timeout time.Duration) {
	var timer *time.Timer
	timer = time.AfterFunc(interval, func() {
		now := time.Now()
		s.m.Range(func(key, value any) bool {
			sink := value.(*M3U8Sink)
			playingTime := sink.GetPlayingTime()
			if now.Sub(playingTime) > timeout {
				log.Sugar.Infof("hls拉流超时 sink: %s  playingTime: %s", sink.GetID(), playingTime.Format("2006-01-02 15:04:05"))
				go sink.Close()
			}

			return true
		})

		timer.Reset(interval)
	})
}
