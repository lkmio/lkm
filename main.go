package main

import (
	"github.com/yangjiechina/live-server/flv"
	"github.com/yangjiechina/live-server/hls"
	"github.com/yangjiechina/live-server/rtsp"
	"net"
	"net/http"

	_ "net/http/pprof"

	"github.com/yangjiechina/avformat/librtmp"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/live-server/rtmp"
	"github.com/yangjiechina/live-server/stream"
)

var rtspAddr *net.TCPAddr

func CreateTransStream(source stream.ISource, protocol stream.Protocol, streams []utils.AVStream) stream.ITransStream {
	if stream.ProtocolRtmp == protocol {
		return rtmp.NewTransStream(librtmp.ChunkSize)
	} else if stream.ProtocolHls == protocol {
		id := source.Id()
		m3u8Name := id + ".m3u8"
		tsFormat := id + "_%d.ts"

		transStream, err := hls.NewTransStream("", m3u8Name, tsFormat, "../tmp/", 2, 10)
		if err != nil {
			panic(err)
		}

		return transStream
	} else if stream.ProtocolFlv == protocol {
		return flv.NewHttpTransStream()
	} else if stream.ProtocolRtsp == protocol {
		trackFormat := source.Id() + "?track=%d"

		return rtsp.NewTransStream(net.IPAddr{
			IP:   rtspAddr.IP,
			Zone: rtspAddr.Zone,
		}, trackFormat)
	}

	return nil
}

func init() {
	stream.TransStreamFactory = CreateTransStream
}

func main() {
	stream.AppConfig.GOPCache = true
	stream.AppConfig.MergeWriteLatency = 350

	rtmpAddr, err := net.ResolveTCPAddr("tcp", "0.0.0.0:1935")
	if err != nil {
		panic(err)
	}

	impl := rtmp.NewServer()
	err = impl.Start(rtmpAddr)
	if err != nil {
		panic(err)
	}

	println("启动rtmp服务成功:" + rtmpAddr.String())

	rtspAddr, err = net.ResolveTCPAddr("tcp", "0.0.0.0:554")
	if err != nil {
		panic(rtspAddr)
	}

	rtspServer := rtsp.NewServer()
	err = rtspServer.Start(rtspAddr)
	if err != nil {
		panic(err)
	}

	println("启动rtsp服务成功:" + rtspAddr.String())

	apiAddr := "0.0.0.0:8080"
	go startApiServer(apiAddr)

	loadConfigError := http.ListenAndServe(":19999", nil)
	if loadConfigError != nil {
		panic(loadConfigError)
	}
	select {}
}
