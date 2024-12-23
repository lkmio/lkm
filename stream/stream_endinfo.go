package stream

import (
	"github.com/lkmio/avformat/libhls"
	"github.com/lkmio/avformat/utils"
	"sync"
)

var (
	streamEndInfoManager *StreamEndInfoManager
)

func init() {
	streamEndInfoManager = &StreamEndInfoManager{sources: make(map[string]*StreamEndInfo, 32)}
}

// StreamEndInfo 保留结束推流Source的推流信息
// 在结束推流时，如果还有拉流端没有断开，则保留一些推流信息(目前有时间戳、ts切片序号等等)。在下次推流时，使用该时间戳作为新传输流的起始时间戳，保证拉流端在拉流时不会重现pts和dts错误.
// 如果在此之前，陆续有拉流端断开，直至sink计数为0，则会不再保留该信息。
type StreamEndInfo struct {
	ID             string
	Timestamps     map[utils.AVCodecID][2]int64 // 每路track结束时间戳
	M3U8Writer     libhls.M3U8Writer
	PlaylistFormat *string
	RtspTracks     map[byte]uint16
}

func EqualsTracks(info *StreamEndInfo, tracks []*Track) bool {
	if len(info.Timestamps) != len(tracks) {
		return false
	}

	for _, track := range tracks {
		if _, ok := info.Timestamps[track.Stream.CodecId()]; !ok {
			return false
		}
	}

	return true
}

type StreamEndInfoManager struct {
	sources map[string]*StreamEndInfo
	lock    sync.RWMutex
}

func (s *StreamEndInfoManager) Add(history *StreamEndInfo) {
	s.lock.Lock()
	defer s.lock.Unlock()

	_, ok := s.sources[history.ID]
	utils.Assert(!ok)

	s.sources[history.ID] = history
}

func (s *StreamEndInfoManager) Remove(id string) *StreamEndInfo {
	s.lock.Lock()
	defer s.lock.Unlock()

	history := s.sources[id]
	delete(s.sources, id)
	return history
}
