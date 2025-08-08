package main

import (
	"encoding/json"
	flv2 "github.com/lkmio/flv"
	"github.com/lkmio/lkm/flv"
	"github.com/lkmio/lkm/hls"
	"github.com/lkmio/lkm/jt1078"
	"github.com/lkmio/lkm/record"
	"github.com/lkmio/lkm/rtsp"
	"github.com/lkmio/lkm/transcode"
	"github.com/lkmio/mpeg"
	"github.com/lkmio/rtp"
	"github.com/lkmio/transport"
	"os"
	"time"

	"github.com/lkmio/lkm/gb28181"
	"github.com/lkmio/lkm/log"
	"github.com/lkmio/lkm/rtc"
	"github.com/lkmio/lkm/rtmp"
	"github.com/lkmio/lkm/stream"
	"go.uber.org/zap/zapcore"
	"net"
	"net/http"
	_ "net/http/pprof"
	"strconv"
)

func init() {
	stream.RegisterTransStreamFactory(stream.TransStreamRtmp, rtmp.TransStreamFactory, flv2.SupportedCodecs)
	stream.RegisterTransStreamFactory(stream.TransStreamHls, hls.TransStreamFactory, mpeg.TSSupportedCodecs)
	stream.RegisterTransStreamFactory(stream.TransStreamFlv, flv.TransStreamFactory, flv2.SupportedCodecs)
	stream.RegisterTransStreamFactory(stream.TransStreamRtsp, rtsp.TransStreamFactory, rtp.SupportedCodecs)
	stream.RegisterTransStreamFactory(stream.TransStreamRtc, rtc.TransStreamFactory, rtc.SupportedCodecs)
	stream.RegisterTransStreamFactory(stream.TransStreamGBCascaded, stream.GBCascadedTransStreamFactory, mpeg.SupportedCodecs)
	stream.RegisterTransStreamFactory(stream.TransStreamGBTalk, gb28181.TalkTransStreamFactory, mpeg.SupportedCodecs)
	stream.RegisterTransStreamFactory(stream.TransStreamGBGateway, gb28181.GatewayTransStreamFactory, mpeg.SupportedCodecs)
	stream.SetRecordStreamFactory(record.NewFLVFileSink)
	stream.StreamEndInfoBride = NewStreamEndInfo

	config, err := stream.LoadConfigFile("./config.json")
	if err != nil {
		panic(err)
	}

	stream.SetDefaultConfig(config)

	options := map[string]stream.EnableConfig{
		"rtmp":    &config.Rtmp,
		"rtsp":    &config.Rtsp,
		"hls":     &config.Hls,
		"webrtc":  &config.WebRtc,
		"gb28181": &config.GB28181,
		"jt1078":  &config.JT1078,
		"hooks":   &config.Hooks,
		"record":  &config.Record,
	}

	// 读取运行参数
	disableOptions, enableOptions := readRunArgs()
	mergeArgs(options, disableOptions, enableOptions)

	stream.AppConfig = *config

	if stream.AppConfig.Hooks.Enable {
		stream.InitHookUrls()
	}

	if stream.AppConfig.WebRtc.Enable {
		// 设置公网IP和端口
		rtc.InitConfig()
	}

	// 初始化日志
	log.InitLogger(config.Log.FileLogging, zapcore.Level(stream.AppConfig.Log.Level), stream.AppConfig.Log.Name, stream.AppConfig.Log.MaxSize, stream.AppConfig.Log.MaxBackup, stream.AppConfig.Log.MaxAge, stream.AppConfig.Log.Compress)

	if stream.AppConfig.GB28181.Enable || stream.AppConfig.Rtsp.Enable {
		transportManager := transport.NewTransportManager(config.ListenIP, uint16(stream.AppConfig.MediaPort[0]), uint16(stream.AppConfig.MediaPort[1]))
		gb28181.TransportManger = transportManager
		rtsp.TransportManger = transportManager
	}

	// 创建dump目录
	if stream.AppConfig.Debug {
		if err := os.MkdirAll("dump", 0666); err != nil {
			panic(err)
		}
	}

	// 打印配置信息
	indent, _ := json.MarshalIndent(stream.AppConfig, "", "\t")
	log.Sugar.Infof("server config:\r\n%s", indent)
}

func main() {
	if stream.AppConfig.Rtmp.Enable {
		rtmpAddr, err := net.ResolveTCPAddr("tcp", stream.ListenAddr(stream.AppConfig.Rtmp.Port))
		if err != nil {
			panic(err)
		}

		server := rtmp.NewServer()
		err = server.Start(rtmpAddr)
		if err != nil {
			panic(err)
		}

		log.Sugar.Info("启动rtmp服务成功 addr:", rtmpAddr.String())
	}

	if stream.AppConfig.Rtsp.Enable {
		rtspAddr, err := net.ResolveTCPAddr("tcp", stream.ListenAddr(stream.AppConfig.Rtsp.Port))
		if err != nil {
			panic(rtspAddr)
		}

		server := rtsp.NewServer(stream.AppConfig.Rtsp.Password)
		err = server.Start(rtspAddr)
		if err != nil {
			panic(err)
		}

		log.Sugar.Info("启动rtsp服务成功 addr:", rtspAddr.String())
	}

	if stream.AppConfig.Hls.Enable {
		// 每10秒检查一次hls拉流超时, 60秒内没有拉流的sink将被关闭
		hls.SinkManager.StartPullStreamTimeoutTimer(10*time.Second, 60*time.Second)
	}

	log.Sugar.Info("启动http服务 addr:", stream.ListenAddr(stream.AppConfig.Http.Port))
	go startApiServer(net.JoinHostPort(stream.AppConfig.ListenIP, strconv.Itoa(stream.AppConfig.Http.Port)))

	// GB28181收流时调用api创建收流端口

	if stream.AppConfig.JT1078.Enable {
		// 无法通过包头区分2016和2019, 每个版本创建一个Server
		ports := [][2]int{{stream.AppConfig.JT1078.Port, 2016}}
		if stream.AppConfig.JT1078.Port2019 > 0 {
			ports = append(ports, [2]int{stream.AppConfig.JT1078.Port2019, 2019})
		}

		for _, port := range ports {
			jtAddr, err := net.ResolveTCPAddr("tcp", stream.ListenAddr(port[0]))
			if err != nil {
				panic(err)
			}

			server := jt1078.NewServer(port[1])
			err = server.Start(jtAddr)
			if err != nil {
				panic(err)
			}

			log.Sugar.Info("启动jt1078服务成功 addr:", jtAddr.String())
		}
	}

	if stream.AppConfig.Hooks.IsEnableOnStarted() {
		go func() {
			_, _ = stream.Hook(stream.HookEventStarted, "", nil)
		}()
	}

	if transcode.CreateAudioTranscoder != nil {
		log.Sugar.Info("启用音频转码功能")
	}

	// 开启pprof调试
	err := http.ListenAndServe(":19999", nil)
	if err != nil {
		println(err)
	}

	select {}
}
