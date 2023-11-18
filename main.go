package main

import (
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/live-server/stream"
)

func CreateTransStream(protocol stream.Protocol, streams []utils.AVStream) stream.ITransStream {
	if stream.ProtocolRtmp == protocol {

	}

	return nil
}

func init() {
	stream.TransStreamFactory = CreateTransStream
}

func main() {

}
