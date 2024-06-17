package stream

import (
	"fmt"
	"github.com/yangjiechina/lkm/log"
	"net/url"
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

// ParseUrl 从推拉流url中解析出流id和url参数
func ParseUrl(name string) (string, url.Values) {
	index := strings.Index(name, "?")
	if index > 0 && index < len(name)-1 {
		query, err := url.ParseQuery(name[index+1:])
		if err != nil {
			log.Sugar.Errorf("解析url参数失败 err:%s url:%s", err.Error(), name)
			return name, nil
		}

		return name[:index], query
	}

	return name, nil
}
