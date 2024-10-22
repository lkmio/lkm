package gb28181

import (
	"encoding/json"
	"fmt"
	"github.com/lkmio/avformat/transport"
	"github.com/lkmio/lkm/stream"
	"net"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"
)

func callForward(source, setup, addr string) string {
	v := &struct {
		Source string `json:"source"` //GetSourceID
		Addr   string `json:"addr"`
		SSRC   uint32 `json:"ssrc"`
		Setup  string `json:"setup"`
	}{
		Source: source,
		Addr:   addr,
		SSRC:   0x100,
		Setup:  setup,
	}

	body, err := json.Marshal(&v)
	if err != nil {
		panic(err)
	}

	response, err := stream.SendHookEvent("http://localhost:8080/api/v1/gb28181/forward", body)
	if err != nil {
		panic(err)
	}

	if response == nil || response.StatusCode != http.StatusOK {
		println("设置级联转发失败")
		return ""
	}

	resp := &struct {
		ID   string `json:"id"` //sink id
		IP   string `json:"ip"`
		Port int    `json:"port"`
	}{}

	bytes := make([]byte, 1024)
	n, err := response.Body.Read(bytes)

	if err != nil && n < 1 {
		panic(err)
	}

	err = json.Unmarshal(bytes[:n], resp)
	if err != nil {
		panic(err)
	}

	return resp.ID
}

func closeForwardSink(source, sink string) {
	v := &struct {
		Sink   string `json:"sink"`
		Source string `json:"source"`
	}{
		Source: source,
		Sink:   sink,
	}

	body, err := json.Marshal(&v)
	if err != nil {
		panic(err)
	}

	_, err = stream.SendHookEvent("http://localhost:8080/api/v1/sink/close", body)
	if err != nil {
		panic(err)
	}
}

func createTransport(setup string) (transport.ITransport, *os.File) {
	var socket transport.ITransport
	name := fmt.Sprintf("./gb_forward_ps_%s_%d.raw", setup, time.Now().UnixMilli())
	file, err := os.OpenFile(name, os.O_CREATE|os.O_RDWR, 0666)
	if err != nil {
		panic(err)
	}

	handler := func(conn net.Conn, data []byte) []byte {
		if setup != "udp" {
			file.Write(data[12:])
		} else {
			file.Write(data[14:])
		}

		return nil
	}

	if "udp" == setup {
		client := transport.UDPClient{}
		err := client.Connect(nil, nil)
		if err != nil {
			panic(err)
		}

		client.SetHandler2(nil, handler, nil)
		go client.Receive()
		socket = &client
	} else if "active" == setup {
		tcpClient := transport.TCPClient{}
		tcpClient.SetHandler2(nil, handler, nil)

		go tcpClient.Receive()
		socket = &tcpClient
	} else if "passive" == setup {
		tcpServer := transport.TCPServer{}
		tcpServer.SetHandler2(nil, handler, nil)
		tcpServer.Bind(nil)

		go tcpServer.Accept()
		socket = &tcpServer
	}

	port := socket.ListenPort()
	fmt.Printf("收流端口:%d\r\n", port)
	return socket, file
}

func TestForwardSink(t *testing.T) {
	source := "34020000001110000001/34020000001310000001"

	for {
		var ids []string
		var transports []transport.ITransport
		var files []*os.File

		// 三种推流方式都测试
		for i := 1; i < 2; i++ {
			var setup string
			if i == 0 {
				setup = "udp"
			} else if i == 1 {
				setup = "passive"
			} else {
				setup = "active"
			}

			// 监听收流端口
			client, out := createTransport(setup)

			// 调用api设置为转发目标
			addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(client.ListenPort()))
			id := callForward(source, setup, addr)

			ids = append(ids, id)
			transports = append(transports, client)
			files = append(files, out)
		}

		time.Sleep(20 * time.Second)

		for i := 0; i < len(ids); i++ {
			if transports[i] != nil {
				transports[i].Close()
			}

			if ids[i] != "" {
				closeForwardSink(source, ids[i])
			}

			if files[i] != nil {
				files[i].Close()
			}
		}
	}

}
