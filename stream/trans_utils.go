package stream

import "github.com/lkmio/avformat/utils"

// TransStreamID 每个传输流的唯一Id，根据输出流协议ID+track index生成
// 输出流协议占低8位
// 每个音视频编译器ID占用8位. 意味着每个输出流至多7路流.
type TransStreamID uint64

func (id TransStreamID) HasTrack(index int) bool {
	for i := 1; i < 8; i++ {
		if int(id>>(i*8))&0xFF == index {
			return true
		}
	}

	return false
}

func (id TransStreamID) Protocol() TransStreamProtocol {
	return TransStreamProtocol(id & 0xFF)
}

// GenerateTransStreamID 根据传入的推拉流协议和编码器ID生成StreamId
// 请确保ids根据值升序排序传参
/*func GenerateTransStreamID(protocol GetProtocol, ids ...utils.AVCodecID) GetTransStreamID {
	len_ := len(ids)
	utils.Assert(len_ > 0 && len_ < 8)

	var streamId uint64
	streamId = uint64(protocol) << 56

	for i, GetID := range ids {
		bId, ok := narrowCodecIds[int(GetID)]
		utils.Assert(ok)

		streamId |= uint64(bId) << (48 - i*8)
	}

	return GetTransStreamID(streamId)
}*/

// GenerateTransStreamID 根据输出流协议和输出流包含的音视频编码器ID生成流ID
func GenerateTransStreamID(protocol TransStreamProtocol, tracks ...*Track) TransStreamID {
	len_ := len(tracks)
	utils.Assert(len_ > 0 && len_ < 8)

	var streamId = uint64(protocol) & 0xFF
	for i, track := range tracks {
		streamId |= uint64(track.Stream.Index) << ((i + 1) * 8)
	}

	return TransStreamID(streamId)
}
