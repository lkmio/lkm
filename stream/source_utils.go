package stream

import (
	"fmt"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SourceType 推流类型
type SourceType byte

// TransStreamProtocol 输出的流协议
type TransStreamProtocol uint32

// SessionState 推拉流Session的状态
type SessionState uint32

const (
	SourceTypeRtmp   = SourceType(1)
	SourceType28181  = SourceType(2)
	SourceType1078   = SourceType(3)
	SourceTypeGBTalk = SourceType(4) // 国标广播/对讲

	TransStreamRtmp       = TransStreamProtocol(1)
	TransStreamFlv        = TransStreamProtocol(2)
	TransStreamRtsp       = TransStreamProtocol(3)
	TransStreamHls        = TransStreamProtocol(4)
	TransStreamRtc        = TransStreamProtocol(5)
	TransStreamGBCascaded = TransStreamProtocol(6) // 国标级联转发
	TransStreamGBTalk     = TransStreamProtocol(7) // 国标广播/对讲转发
	TransStreamGBGateway  = TransStreamProtocol(8) // 国标网关
)

const (
	SessionStateCreated          = SessionState(1) // 新建状态
	SessionStateHandshaking      = SessionState(2) // 握手中
	SessionStateHandshakeFailure = SessionState(3) // 握手失败
	SessionStateHandshakeSuccess = SessionState(4) // 握手完成
	SessionStateWaiting          = SessionState(5) // 位于等待队列中
	SessionStateTransferring     = SessionState(6) // 推拉流中
	SessionStateClosed           = SessionState(7) // 关闭状态
)

func (s SourceType) String() string {
	if SourceTypeRtmp == s {
		return "rtmp"
	} else if SourceType28181 == s {
		return "gb28181"
	} else if SourceType1078 == s {
		return "jt1078"
	} else if SourceTypeGBTalk == s {
		return "gb_talk"
	}

	panic(fmt.Sprintf("unknown source type %d", s))
}

func (p TransStreamProtocol) String() string {
	if TransStreamRtmp == p {
		return "rtmp"
	} else if TransStreamFlv == p {
		return "flv"
	} else if TransStreamRtsp == p {
		return "rtsp"
	} else if TransStreamHls == p {
		return "hls"
	} else if TransStreamRtc == p {
		return "rtc"
	} else if TransStreamGBCascaded == p {
		return "gb_cascaded"
	} else if TransStreamGBTalk == p {
		return "gb_talk"
	} else if TransStreamGBGateway == p {
		return "gb_gateway"
	}

	panic(fmt.Sprintf("unknown stream protocol %d", p))
}

func (s SessionState) String() string {
	if SessionStateCreated == s {
		return "create"
	} else if SessionStateHandshaking == s {
		return "handshaking"
	} else if SessionStateHandshakeFailure == s {
		return "handshake failure"
	} else if SessionStateHandshakeSuccess == s {
		return "handshake success"
	} else if SessionStateWaiting == s {
		return "waiting"
	} else if SessionStateTransferring == s {
		return "transferring"
	} else if SessionStateClosed == s {
		return "closed"
	}

	panic(fmt.Sprintf("unknown session state %d", s))
}

func Path2SourceID(path string, suffix string) (string, error) {
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

// StartReceiveDataTimer 启动收流超时计时器
// 收流超时, 客观上认为是流中断, 应该关闭Source. 如果开启了Hook, 并且Hook返回200应答, 则不关闭Source.
func StartReceiveDataTimer(source Source) *time.Timer {
	utils.Assert(AppConfig.ReceiveTimeout > 0)

	var receiveDataTimer *time.Timer
	receiveDataTimer = time.AfterFunc(time.Duration(AppConfig.ReceiveTimeout), func() {
		dis := time.Now().Sub(source.LastPacketTime())

		if dis >= time.Duration(AppConfig.ReceiveTimeout) {
			log.Sugar.Errorf("收流超时 source: %s", source.GetID())

			var shouldClose = true
			if AppConfig.Hooks.IsEnableOnReceiveTimeout() {
				// 此处参考返回值err, 客观希望关闭Source
				response, err := HookReceiveTimeoutEvent(source)
				shouldClose = !(err == nil && response != nil && http.StatusOK == response.StatusCode)
			}

			if shouldClose {
				source.Close()
				return
			}
		}

		// 对精度没要求
		receiveDataTimer.Reset(time.Duration(AppConfig.ReceiveTimeout))
	})

	return receiveDataTimer
}

// StartIdleTimer 启动拉流空闲计时器
// 拉流空闲, 不应该关闭Source. 如果开启了Hook, 并且Hook返回非200应答, 则关闭Source.
func StartIdleTimer(source Source) *time.Timer {
	utils.Assert(AppConfig.IdleTimeout > 0)
	utils.Assert(AppConfig.Hooks.IsEnableOnIdleTimeout())

	var idleTimer *time.Timer
	idleTimer = time.AfterFunc(time.Duration(AppConfig.IdleTimeout), func() {
		dis := time.Now().Sub(source.GetTransStreamPublisher().LastStreamEndTime())

		if source.GetTransStreamPublisher().SinkCount() < 1 && dis >= time.Duration(AppConfig.IdleTimeout) {
			log.Sugar.Errorf("拉流空闲超时 source: %s", source.GetID())

			// 此处不参考返回值err, 客观希望不关闭Source
			response, _ := HookIdleTimeoutEvent(source)
			if response != nil && http.StatusOK != response.StatusCode {
				source.Close()
				return
			}
		}

		idleTimer.Reset(time.Duration(AppConfig.IdleTimeout))
	})

	return idleTimer
}

func CloseSource(id string) {
	source := SourceManager.Find(id)
	if source != nil {
		source.Close()
	}
}

// LoopEvent 循环读取事件
func LoopEvent(source Source) {
	source.StartTimers(source)
	source.GetTransStreamPublisher().start()
}
