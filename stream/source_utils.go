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
	SourceTypeRtmp  = SourceType(1)
	SourceType28181 = SourceType(2)
	SourceType1078  = SourceType(3)

	TransStreamRtmp            = TransStreamProtocol(1)
	TransStreamFlv             = TransStreamProtocol(2)
	TransStreamRtsp            = TransStreamProtocol(3)
	TransStreamHls             = TransStreamProtocol(4)
	TransStreamRtc             = TransStreamProtocol(5)
	TransStreamGBStreamForward = TransStreamProtocol(6) // 国标级联转发
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
		return "28181"
	} else if SourceType1078 == s {
		return "jt1078"
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
	} else if TransStreamGBStreamForward == p {
		return "gb_stream_forward"
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

//
//func ExtractVideoPacket(codec utils.AVCodecID, key, extractStream bool, data []byte, pts, dts int64, index, timebase int) (*avformat.AVStream, *avformat.AVPacket, error) {
//	var stream *avformat.AVStream
//
//	if utils.AVCodecIdH264 == codec {
//		//从关键帧中解析出sps和pps
//		if key && extractStream {
//			sps, pps, err := avc.ParseExtraDataFromKeyNALU(data)
//			if err != nil {
//				log.Sugar.Errorf("从关键帧中解析sps pps失败 data:%s", hex.EncodeToString(data))
//				return nil, nil, err
//			}
//
//			codecData, err := utils.NewAVCCodecData(sps, pps)
//			if err != nil {
//				log.Sugar.Errorf("解析sps pps失败 data:%s sps:%s, pps:%s", hex.EncodeToString(data), hex.EncodeToString(sps), hex.EncodeToString(pps))
//				return nil, nil, err
//			}
//
//			stream = avformat.NewAVStream(utils.AVMediaTypeVideo, 0, codec, codecData.AnnexBExtraData(), codecData)
//		}
//
//	} else if utils.AVCodecIdH265 == codec {
//		if key && extractStream {
//			vps, sps, pps, err := hevc.ParseExtraDataFromKeyNALU(data)
//			if err != nil {
//				log.Sugar.Errorf("从关键帧中解析vps sps pps失败  data:%s", hex.EncodeToString(data))
//				return nil, nil, err
//			}
//
//			codecData, err := utils.NewHEVCCodecData(vps, sps, pps)
//			if err != nil {
//				log.Sugar.Errorf("解析sps pps失败 data:%s vps:%s sps:%s, pps:%s", hex.EncodeToString(data), hex.EncodeToString(vps), hex.EncodeToString(sps), hex.EncodeToString(pps))
//				return nil, nil, err
//			}
//
//			stream = avformat.NewAVStream(utils.AVMediaTypeVideo, 0, codec, codecData.AnnexBExtraData(), codecData)
//		}
//
//	}
//
//	packet := avformat.NewVideoPacket(data, dts, pts, key, utils.PacketTypeAnnexB, codec, index, timebase)
//	return stream, packet, nil
//}
//
//func ExtractAudioPacket(codec utils.AVCodecID, extractStream bool, data []byte, pts, dts int64, index, timebase int) (*avformat.AVStream, *avformat.AVPacket, error) {
//	var stream *avformat.AVStream
//	var packet *avformat.AVPacket
//	if utils.AVCodecIdAAC == codec {
//		//必须包含ADTSHeader
//		if len(data) < 7 {
//			return nil, nil, fmt.Errorf("need more data")
//		}
//
//		var skip int
//		header, err := utils.ReadADtsFixedHeader(data)
//		if err != nil {
//			log.Sugar.Errorf("读取ADTSHeader失败 data:%s", hex.EncodeToString(data[:7]))
//			return nil, nil, err
//		} else {
//			skip = 7
//			//跳过ADtsHeader长度
//			if header.ProtectionAbsent() == 0 {
//				skip += 2
//			}
//		}
//
//		if extractStream {
//			configData, err := utils.ADtsHeader2MpegAudioConfigData(header)
//			config, err := utils.ParseMpeg4AudioConfig(configData)
//			println(config)
//			if err != nil {
//				log.Sugar.Errorf("adt头转m4ac失败 data:%s", hex.EncodeToString(data[:7]))
//				return nil, nil, err
//			}
//
//			stream = avformat.NewAVStream(utils.AVMediaTypeAudio, index, codec, configData, nil)
//		}
//
//		packet = utils.NewAudioPacket(data[skip:], dts, pts, codec, index, timebase)
//	} else if utils.AVCodecIdPCMALAW == codec || utils.AVCodecIdPCMMULAW == codec {
//		if extractStream {
//			stream = avformat.NewAVStream(utils.AVMediaTypeAudio, index, codec, nil, nil)
//		}
//
//		packet = utils.NewAudioPacket(data, dts, pts, codec, index, timebase)
//	}
//
//	return stream, packet, nil
//}

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
		dis := time.Now().Sub(source.LastStreamEndTime())

		if source.SinkCount() < 1 && dis >= time.Duration(AppConfig.IdleTimeout) {
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

// LoopEvent 循环读取事件
func LoopEvent(source Source) {
	// 将超时计时器放在此处开启, 方便在退出的时候关闭
	var receiveTimer *time.Timer
	var idleTimer *time.Timer
	var probeTimer *time.Timer

	defer func() {
		log.Sugar.Debugf("主协程执行结束 source: %s", source.GetID())

		// 关闭计时器
		if receiveTimer != nil {
			receiveTimer.Stop()
		}

		if idleTimer != nil {
			idleTimer.Stop()
		}

		if probeTimer != nil {
			probeTimer.Stop()
		}

		// 未使用的数据, 放回池中
		for len(source.StreamPipe()) > 0 {
			data := <-source.StreamPipe()
			if size := cap(data); size > UDPReceiveBufferSize {
				TCPReceiveBufferPool.Put(data[:size])
			} else {
				UDPReceiveBufferPool.Put(data[:size])
			}
		}
	}()

	// 开启收流超时计时器
	if AppConfig.ReceiveTimeout > 0 {
		receiveTimer = StartReceiveDataTimer(source)
	}

	// 开启拉流空闲超时计时器
	if AppConfig.Hooks.IsEnableOnIdleTimeout() && AppConfig.IdleTimeout > 0 {
		idleTimer = StartIdleTimer(source)
	}

	// 开启探测超时计时器
	probeTimer = time.AfterFunc(time.Duration(AppConfig.ProbeTimeout)*time.Millisecond, func() {
		if source.IsCompleted() {
			return
		}

		var ok bool
		source.PostEvent(func() {
			source.ProbeTimeout()
			ok = len(source.OriginTracks()) > 0
		})

		if !ok {
			source.Close()
			return
		}
	})

	for {
		select {
		// 读取推流数据
		case data := <-source.StreamPipe():
			if AppConfig.ReceiveTimeout > 0 {
				source.SetLastPacketTime(time.Now())
			}

			if err := source.Input(data); err != nil {
				log.Sugar.Errorf("解析推流数据发生err: %s 释放source: %s", err.Error(), source.GetID())
				go source.Close()
				return
			}

			// 使用后, 放回池中
			if size := cap(data); size > UDPReceiveBufferSize {
				TCPReceiveBufferPool.Put(data[:size])
			} else {
				UDPReceiveBufferPool.Put(data[:size])
			}
			break
		// 切换到主协程,执行该函数. 目的是用于无锁化处理推拉流的连接与断开, 推流源断开, 查询推流源信息等事件. 不要做耗时操作, 否则会影响推拉流.
		case event := <-source.MainContextEvents():
			event()

			if source.IsClosed() {
				// 处理推流管道剩余的数据?
				return
			}

			break
		}
	}
}
