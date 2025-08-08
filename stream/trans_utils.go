package stream

import "github.com/lkmio/avformat/utils"

// TransStreamID 每个传输流的唯一Id, 根据输出流协议ID+track index生成
// 输出流协议占低8位, track index占用8位, 最多支持7路流.
type TransStreamID uint64

func (id TransStreamID) HasTrack(index int) bool {
	for i := 1; i < 8; i++ {
		if (int(id>>(i*8))&0xFF)-1 == index {
			return true
		}
	}

	return false
}

func (id TransStreamID) Protocol() TransStreamProtocol {
	return TransStreamProtocol(id & 0xFF)
}

// GenerateTransStreamID 根据输出流协议和输出流包含的音视频编码器ID生成流ID
func GenerateTransStreamID(protocol TransStreamProtocol, tracks ...*Track) TransStreamID {
	len_ := len(tracks)
	utils.Assert(len_ > 0 && len_ < 8)

	var streamId = uint64(protocol) & 0xFF
	for i, track := range tracks {
		// +1是为了避免0值
		streamId |= uint64(track.Stream.Index+1) << ((i + 1) * 8)
	}

	return TransStreamID(streamId)
}
