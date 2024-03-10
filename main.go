package main

import (
	"github.com/yangjiechina/live-server/flv"
	"github.com/yangjiechina/live-server/hls"
	"net"
	"net/http"

	_ "net/http/pprof"

	"github.com/yangjiechina/avformat/librtmp"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/live-server/rtmp"
	"github.com/yangjiechina/live-server/stream"
)

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
	}

	return nil
}

func init() {
	stream.TransStreamFactory = CreateTransStream
}

func main() {
	stream.AppConfig.GOPCache = true
	stream.AppConfig.MergeWriteLatency = 350
	impl := rtmp.NewServer()
	addr := "0.0.0.0:1935"
	tcpAddr, err := net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		panic(err)
	}

	err = impl.Start(tcpAddr)
	if err != nil {
		panic(err)
	}

	println("启动rtmp服务成功:" + addr)

	apiAddr := "0.0.0.0:8080"
	go startApiServer(apiAddr)

	loadConfigError := http.ListenAndServe(":19999", nil)
	if loadConfigError != nil {
		panic(loadConfigError)
	}
	select {}
}
