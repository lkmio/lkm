package rtmp

import (
	"github.com/yangjiechina/avformat/librtmp"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/lkm/stream"
	"net"
	"testing"
)

func CreateTransStream(source stream.ISource, protocol stream.Protocol, streams []utils.AVStream) stream.ITransStream {
	if stream.ProtocolRtmp == protocol {
		return NewTransStream(librtmp.ChunkSize)
	}

	return nil
}

func init() {
	stream.TransStreamFactory = CreateTransStream
}

func TestServer(t *testing.T) {
	stream.AppConfig.GOPCache = true
	stream.AppConfig.MergeWriteLatency = 350
	impl := serverImpl{}
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
	select {}
}
