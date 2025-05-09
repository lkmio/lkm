package gb28181

import (
	"github.com/lkmio/lkm/stream"
)

func CascadedTransStreamFactory(source stream.Source, protocol stream.TransStreamProtocol, tracks []*stream.Track) (stream.TransStream, error) {
	return stream.NewRtpTransStream(stream.TransStreamGBCascadedForward, 1024), nil
}
