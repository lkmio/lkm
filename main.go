package main

import (
	"encoding/json"
	"github.com/lkmio/avformat/transport"
	"github.com/lkmio/lkm/flv"
	"github.com/lkmio/lkm/gb28181"
	"github.com/lkmio/lkm/hls"
	"github.com/lkmio/lkm/jt1078"
	"github.com/lkmio/lkm/log"
	"github.com/lkmio/lkm/rtc"
	"github.com/lkmio/lkm/rtsp"
	"go.uber.org/zap/zapcore"
	"net"
	"net/http"
	_ "net/http/pprof"
	"strconv"

	"github.com/lkmio/lkm/rtmp"
	"github.com/lkmio/lkm/stream"
)

func init() {
	stream.RegisterTransStreamFactory(stream.ProtocolRtmp, rtmp.TransStreamFactory)
	stream.RegisterTransStreamFactory(stream.ProtocolHls, hls.TransStreamFactory)
	stream.RegisterTransStreamFactory(stream.ProtocolFlv, flv.TransStreamFactory)
	stream.RegisterTransStreamFactory(stream.ProtocolRtsp, rtsp.TransStreamFactory)
	stream.RegisterTransStreamFactory(stream.ProtocolRtc, rtc.TransStreamFactory)

	config, err := stream.LoadConfigFile("./config.json")
	if err != nil {
		panic(err)
	}

	stream.SetDefaultConfig(config)
	stream.AppConfig = *config
	stream.InitHookUrl()

	//初始化日志
	log.InitLogger(zapcore.Level(stream.AppConfig.Log.Level), stream.AppConfig.Log.Name, stream.AppConfig.Log.MaxSize, stream.AppConfig.Log.MaxBackup, stream.AppConfig.Log.MaxAge, stream.AppConfig.Log.Compress)

	if stream.AppConfig.GB28181.IsMultiPort() {
		gb28181.TransportManger = transport.NewTransportManager(uint16(stream.AppConfig.GB28181.Port[0]), uint16(stream.AppConfig.GB28181.Port[1]))
	}

	if stream.AppConfig.Rtsp.IsMultiPort() {
		rtsp.TransportManger = transport.NewTransportManager(uint16(stream.AppConfig.Rtsp.Port[1]), uint16(stream.AppConfig.Rtsp.Port[2]))
	}

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
		rtspAddr, err := net.ResolveTCPAddr("tcp", stream.ListenAddr(stream.AppConfig.Rtsp.Port[0]))
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

	log.Sugar.Info("启动Http服务 addr:", stream.ListenAddr(stream.AppConfig.Http.Port))
	go startApiServer(net.JoinHostPort(stream.AppConfig.ListenIP, strconv.Itoa(stream.AppConfig.Http.Port)))

	//单端口模式下, 启动时就创建收流端口
	//多端口模式下, 创建GBSource时才创建收流端口
	if !stream.AppConfig.GB28181.IsMultiPort() {
		if stream.AppConfig.GB28181.IsEnableUDP() {
			server, err := gb28181.NewUDPServer(gb28181.NewSharedFilter(128))
			if err != nil {
				panic(err)
			}

			gb28181.SharedUDPServer = server
			log.Sugar.Info("启动GB28181 UDP收流端口成功:" + stream.ListenAddr(stream.AppConfig.GB28181.Port[0]))
		}

		if stream.AppConfig.GB28181.IsEnableTCP() {
			server, err := gb28181.NewTCPServer(gb28181.NewSharedFilter(128))
			if err != nil {
				panic(err)
			}

			gb28181.SharedTCPServer = server
			log.Sugar.Info("启动GB28181 TCP收流端口成功:" + stream.ListenAddr(stream.AppConfig.GB28181.Port[0]))
		}
	}

	if stream.AppConfig.JT1078.Enable {
		jtAddr, err := net.ResolveTCPAddr("tcp", stream.ListenAddr(stream.AppConfig.JT1078.Port))
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

	if stream.AppConfig.Hook.IsEnableOnStarted() {
		go func() {
			if _, err := stream.Hook(stream.HookEventStarted, "", nil); err != nil {
				log.Sugar.Errorf("发送启动通知失败 err:%s", err.Error())
			}
		}()
	}

	err := http.ListenAndServe(":19999", nil)
	if err != nil {
		println(err)
	}

	select {}
}
