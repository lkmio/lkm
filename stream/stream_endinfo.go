package stream

import (
	"github.com/lkmio/avformat/utils"
	"sync"
)

var (
	streamEndInfoManager *StreamEndInfoManager
)

func init() {
	streamEndInfoManager = &StreamEndInfoManager{sources: make(map[string]*StreamEndInfo, 32)}
}

// StreamEndInfo 保存Source结束推流时的推流信息
// 在结束推流时，如果还有拉流端没有断开，则保留一些推流信息(时间戳、ts切片序号等等)。在下次推流时，使用该时间戳作为新传输流的起始时间戳，保证拉流端在拉流时不会出现pts和dts错误.
// 如果重新推流之前，陆续有拉流端断开，直至sink计数为0，删除保存的推流信息。
type StreamEndInfo struct {
	ID             string
	Timestamps     map[utils.AVCodecID][2]int64 // 每路track结束时间戳
	M3U8Writer     M3U8Writer                   // 保存M3U8生成器
	PlaylistFormat *string                      // M3U8播放列表
	RtspTracks     map[int]uint16               // rtsp每路track的结束序号
	FLVPrevTagSize uint32                       // flv的最后一个tag大小, 下次生成flv时作为prev tag size
}

func EqualsTracks(info *StreamEndInfo, tracks []*Track) bool {
	if len(info.Timestamps) != len(tracks) {
		return false
	}

	for _, track := range tracks {
		if _, ok := info.Timestamps[track.Stream.CodecID]; !ok {
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
