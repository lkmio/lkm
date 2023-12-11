package main

import (
	"net"
	"net/http"

	_ "net/http/pprof"

	"github.com/yangjiechina/avformat/librtmp"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/live-server/rtmp"
	"github.com/yangjiechina/live-server/stream"
)

func CreateTransStream(protocol stream.Protocol, streams []utils.AVStream) stream.ITransStream {
	if stream.ProtocolRtmp == protocol {
		return rtmp.NewTransStream(librtmp.ChunkSize)
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

	loadConfigError := http.ListenAndServe(":19999", nil)
	if loadConfigError != nil {
		panic(loadConfigError)
	}
	select {}
}
