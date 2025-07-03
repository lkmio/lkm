package transcode

import (
	"github.com/lkmio/avformat"
	"github.com/lkmio/avformat/utils"
)

var (
	CreateAudioTranscoder func(src *avformat.AVStream, dst []utils.AVCodecID) (Transcoder, *avformat.AVStream, error)
)

type Transcoder interface {
	Transcode(src *avformat.AVPacket, cb func([]byte, int)) (int, error)

	GetEncoderID() utils.AVCodecID

	Close()
}
