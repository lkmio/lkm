package stream

import "github.com/yangjiechina/avformat/utils"

// TransStreamId 每个传输流的唯一Id，根据输出流协议ID+流包含的音视频编码器ID生成
// 输出流协议ID占用高8位
// 每个音视频编译器ID占用8位. 意味着每个输出流至多7路流.
type TransStreamId uint64

var (
	// AVCodecID转为byte的对应关系
	narrowCodecIds map[int]byte
)

func init() {
	narrowCodecIds = map[int]byte{
		int(utils.AVCodecIdH263): 0x1,
		int(utils.AVCodecIdH264): 0x2,
		int(utils.AVCodecIdH265): 0x3,
		int(utils.AVCodecIdAV1):  0x4,
		int(utils.AVCodecIdVP8):  0x5,
		int(utils.AVCodecIdVP9):  0x6,

		int(utils.AVCodecIdAAC):      101,
		int(utils.AVCodecIdMP3):      102,
		int(utils.AVCodecIdOPUS):     103,
		int(utils.AVCodecIdPCMALAW):  104,
		int(utils.AVCodecIdPCMMULAW): 105,
	}
}

// GenerateTransStreamId 根据传入的推拉流协议和编码器ID生成StreamId
// 请确保ids根据值升序排序传参
/*func GenerateTransStreamId(protocol Protocol, ids ...utils.AVCodecID) TransStreamId {
	len_ := len(ids)
	utils.Assert(len_ > 0 && len_ < 8)

	var streamId uint64
	streamId = uint64(protocol) << 56

	for i, id := range ids {
		bId, ok := narrowCodecIds[int(id)]
		utils.Assert(ok)

		streamId |= uint64(bId) << (48 - i*8)
	}

	return TransStreamId(streamId)
}*/

// GenerateTransStreamId 根据输出流协议和输出流包含的音视频编码器ID生成流ID
func GenerateTransStreamId(protocol Protocol, ids ...utils.AVStream) TransStreamId {
	len_ := len(ids)
	utils.Assert(len_ > 0 && len_ < 8)

	var streamId uint64
	streamId = uint64(protocol) << 56

	for i, id := range ids {
		bId, ok := narrowCodecIds[int(id.CodecId())]
		utils.Assert(ok)

		streamId |= uint64(bId) << (48 - i*8)
	}

	return TransStreamId(streamId)
}
