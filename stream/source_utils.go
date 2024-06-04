package stream

import (
	"fmt"
	"strings"
)

func (s SourceType) ToString() string {
	if SourceTypeRtmp == s {
		return "rtmp"
	} else if SourceType28181 == s {
		return "28181"
	} else if SourceType1078 == s {
		return "jt1078"
	}

	panic(fmt.Sprintf("unknown source type %d", s))
}

func (p Protocol) ToString() string {
	if ProtocolRtmp == p {
		return "rtmp"
	} else if ProtocolFlv == p {
		return "flv"
	} else if ProtocolRtsp == p {
		return "rtsp"
	} else if ProtocolHls == p {
		return "hls"
	} else if ProtocolRtc == p {
		return "rtc"
	}

	panic(fmt.Sprintf("unknown stream protocol %d", p))
}

func Path2SourceId(path string, suffix string) (string, error) {
	source := strings.TrimSpace(path)
	if strings.HasPrefix(source, "/") {
		source = source[1:]
	}

	if len(suffix) > 0 && strings.HasSuffix(source, suffix) {
		source = source[:len(source)-len(suffix)]
	}

	source = strings.TrimSpace(source)

	if len(strings.TrimSpace(source)) == 0 {
		return "", fmt.Errorf("the request source cannot be empty")
	}

	return source, nil
}
