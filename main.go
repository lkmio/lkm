package main

import (
	"fmt"
	"github.com/yangjiechina/live-server/flv"
	"github.com/yangjiechina/live-server/gb28181"
	"github.com/yangjiechina/live-server/hls"
	"github.com/yangjiechina/live-server/jt1078"
	"github.com/yangjiechina/live-server/log"
	"github.com/yangjiechina/live-server/rtc"
	"github.com/yangjiechina/live-server/rtsp"
	"go.uber.org/zap/zapcore"
	"net"
	"net/http"

	_ "net/http/pprof"

	"github.com/yangjiechina/live-server/rtmp"
	"github.com/yangjiechina/live-server/stream"
)

func NewDefaultAppConfig() stream.AppConfig_ {
	return stream.AppConfig_{
		GOPCache:          true,
		MergeWriteLatency: 350,

		Hls: stream.HlsConfig{
			Enable:         false,
			Dir:            "../tmp",
			Duration:       2,
			PlaylistLength: 10,
		},

		Rtmp: stream.RtmpConfig{
			Enable: true,
			Addr:   "0.0.0.0:1935",
		},

		Rtsp: stream.RtmpConfig{
			Enable: true,
			Addr:   "0.0.0.0:554",
		},

		Log: stream.LogConfig{
			Level:     int(zapcore.DebugLevel),
			Name:      "./logs/lkm.log",
			MaxSize:   10,
			MaxBackup: 100,
			MaxAge:    7,
			Compress:  false,
		},

		Http: stream.HttpConfig{
			Enable: true,
			Addr:   "0.0.0.0:8080",
		},

		GB28181: stream.GB28181Config{
			Addr:      "0.0.0.0",
			Transport: "UDP|TCP",
			Port:      [2]uint16{20000, 30000},
		},

		JT1078: stream.JT1078Config{
			Enable: true,
			Addr:   "0.0.0.0:1078",
		},
	}
}

func init() {
	stream.RegisterTransStreamFactory(stream.ProtocolRtmp, rtmp.TransStreamFactory)
	stream.RegisterTransStreamFactory(stream.ProtocolHls, hls.TransStreamFactory)
	stream.RegisterTransStreamFactory(stream.ProtocolFlv, flv.TransStreamFactory)
	stream.RegisterTransStreamFactory(stream.ProtocolRtsp, rtsp.TransStreamFactory)
	stream.RegisterTransStreamFactory(stream.ProtocolRtc, rtc.TransStreamFactory)

	stream.AppConfig = NewDefaultAppConfig()

	//初始化日志
	log.InitLogger(zapcore.Level(stream.AppConfig.Log.Level), stream.AppConfig.Log.Name, stream.AppConfig.Log.MaxSize, stream.AppConfig.Log.MaxBackup, stream.AppConfig.Log.MaxAge, stream.AppConfig.Log.Compress)

	if stream.AppConfig.GB28181.IsMultiPort() {
		gb28181.TransportManger = stream.NewTransportManager(stream.AppConfig.GB28181.Port[0], stream.AppConfig.GB28181.Port[1])
	}
}

func main() {
	if stream.AppConfig.Rtmp.Enable {
		rtmpAddr, err := net.ResolveTCPAddr("tcp", stream.AppConfig.Rtmp.Addr)
		if err != nil {
			panic(err)
		}

		impl := rtmp.NewServer()
		err = impl.Start(rtmpAddr)
		if err != nil {
			panic(err)
		}

		log.Sugar.Info("启动rtmp服务成功 addr:", rtmpAddr.String())
	}

	if stream.AppConfig.Rtsp.Enable {
		rtspAddr, err := net.ResolveTCPAddr("tcp", stream.AppConfig.Rtsp.Addr)
		if err != nil {
			panic(rtspAddr)
		}

		rtspServer := rtsp.NewServer()
		err = rtspServer.Start(rtspAddr)
		if err != nil {
			panic(err)
		}

		log.Sugar.Info("启动rtsp服务成功 addr:", rtspAddr.String())
	}

	if stream.AppConfig.Http.Enable {
		log.Sugar.Info("启动Http服务 addr:", stream.AppConfig.Http.Addr)

		go startApiServer(stream.AppConfig.Http.Addr)
	}

	//单端口模式下, 启动时就创建收流端口
	//多端口模式下, 创建GBSource时才创建收流端口
	if !stream.AppConfig.GB28181.IsMultiPort() {
		if stream.AppConfig.GB28181.EnableUDP() {
			addr := fmt.Sprintf("%s:%d", stream.AppConfig.GB28181.Addr, stream.AppConfig.GB28181.Port[0])
			gbAddr, err := net.ResolveUDPAddr("udp", addr)
			if err != nil {
				panic(err)
			}

			server, err := gb28181.NewUDPServer(gbAddr, gb28181.NewSharedFilter(128))
			if err != nil {
				panic(err)
			}

			gb28181.SharedUDPServer = server
			log.Sugar.Info("启动GB28181 UDP收流端口成功:" + gbAddr.String())
		}

		if stream.AppConfig.GB28181.EnableTCP() {
			addr := fmt.Sprintf("%s:%d", stream.AppConfig.GB28181.Addr, stream.AppConfig.GB28181.Port[0])
			gbAddr, err := net.ResolveTCPAddr("tcp", addr)
			if err != nil {
				panic(err)
			}

			server, err := gb28181.NewTCPServer(gbAddr, gb28181.NewSharedFilter(128))
			if err != nil {
				panic(err)
			}

			gb28181.SharedTCPServer = server
			log.Sugar.Info("启动GB28181 TCP收流端口成功:" + gbAddr.String())
		}
	}

	if stream.AppConfig.JT1078.Enable {
		jtAddr, err := net.ResolveTCPAddr("tcp", stream.AppConfig.JT1078.Addr)
		if err != nil {
			panic(err)
		}

		server := jt1078.NewServer()
		err = server.Start(jtAddr)
		if err != nil {
			panic(err)
		}

		log.Sugar.Info("启动jt1078服务成功 addr:", jtAddr.String())
	}

	loadConfigError := http.ListenAndServe(":19999", nil)
	if loadConfigError != nil {
		panic(loadConfigError)
	}

	select {}
}
