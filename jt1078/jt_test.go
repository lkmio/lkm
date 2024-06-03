package jt1078

import (
	"github.com/yangjiechina/avformat/libbufio"
	"github.com/yangjiechina/avformat/transport"
	"net"
	"os"
	"testing"
	"time"
)

func TestPublish(t *testing.T) {
	path := "D:\\GOProjects\\avformat\\10352264314-2.bin"

	client := transport.TCPClient{}
	addr, err := net.ResolveTCPAddr("tcp", "127.0.0.1:1078")
	if err != nil {
		panic(err)
	}
	err = client.Connect(nil, addr)
	if err != nil {
		panic(err)
	}

	file, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}

	index := 0
	for index < len(file) {
		n := libbufio.MinInt(len(file)-index, 1500)
		client.Write(file[index : index+n])
		index += n
		time.Sleep(1 * time.Millisecond)
	}

	println("end")
}
