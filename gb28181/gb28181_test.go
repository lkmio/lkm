package gb28181

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/yangjiechina/avformat/transport"
	"io"
	"net"
	"net/http"
	"os"
	"testing"
	"time"
)

// 输入rtp负载的ps流文件路径, 根据ssrc解析, rtp头不要带扩展
func readRtp(path string, ssrc uint32, tcp bool, cb func([]byte)) {
	file, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}

	var offset int
	tcpRtp := make([]byte, 1500)

	for i := 0; i < len(file)-4; i++ {
		if ssrc != binary.BigEndian.Uint32(file[i:]) {
			continue
		}

		if i-8 != 0 {
			var err error
			rtp := file[offset : i-8]

			if tcp {
				binary.BigEndian.PutUint16(tcpRtp, uint16(len(rtp)))
				copy(tcpRtp[2:], rtp)
				cb(tcpRtp[:2+len(rtp)])
			} else {
				cb(rtp)
			}

			if err != nil {
				panic(err.Error())
			}
		}
		offset = i - 8
	}
}

func connectSource(source string, addr string) {
	v := &struct {
		Source     string `json:"source"` //SourceId
		RemoteAddr string `json:"remote_addr"`
	}{
		Source:     source,
		RemoteAddr: addr,
	}

	marshal, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}

	request, err := http.NewRequest("POST", "http://localhost:8080/v1/gb28181/source/connect", bytes.NewBuffer(marshal))
	if err != nil {
		panic(err)
	}

	client := http.Client{}
	response, err := client.Do(request)
	if err != nil {
		panic(err)
	}

	_, err = io.ReadAll(response.Body)
	if err != nil {
		panic(err)
	}
}

func createSource(source, transport, setup string, ssrc uint32) int {
	v := struct {
		Source    string `json:"source"` //SourceId
		Transport string `json:"transport,omitempty"`
		Setup     string `json:"setup"` //active/passive
		SSRC      uint32 `json:"ssrc,omitempty"`
	}{
		Source:    source,
		Transport: transport,
		Setup:     setup,
		SSRC:      ssrc,
	}

	marshal, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}

	request, err := http.NewRequest("POST", "http://localhost:8080/v1/gb28181/source/create", bytes.NewBuffer(marshal))
	if err != nil {
		panic(err)
	}

	client := http.Client{}
	response, err := client.Do(request)
	if err != nil {
		panic(err)
	}

	all, err := io.ReadAll(response.Body)
	if err != nil {
		panic(err)
	}

	resposne := &struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Port int `json:"port"`
		} `json:"data"`
	}{}

	err = json.Unmarshal(all, resposne)
	if err != nil {
		panic(err)
	}

	if resposne.Code != http.StatusOK {
		panic("")
	}

	return resposne.Data.Port
}

func TestUDPRecv(t *testing.T) {
	path := "D:\\GOProjects\\avformat\\gb28181_h264.rtp"
	ssrc := 0xBEBC201
	ip := "192.168.2.148"
	localAddr := "0.0.0.0:20001"
	network := "tcp"
	setup := "passive"
	id := "hls_mystream"

	port := createSource(id, network, setup, uint32(ssrc))

	if network == "udp" {
		addr, _ := net.ResolveUDPAddr(network, localAddr)
		remoteAddr, _ := net.ResolveUDPAddr(network, fmt.Sprintf("%s:%d", ip, port))

		client := &transport.UDPClient{}
		err := client.Connect(addr, remoteAddr)
		if err != nil {
			panic(err)
		}

		readRtp(path, uint32(ssrc), false, func(data []byte) {
			client.Write(data)
			time.Sleep(1 * time.Millisecond)
		})
	} else if !(setup == "active") {
		addr, _ := net.ResolveTCPAddr(network, localAddr)
		remoteAddr, _ := net.ResolveTCPAddr(network, fmt.Sprintf("%s:%d", ip, port))

		client := transport.TCPClient{}
		err := client.Connect(addr, remoteAddr)

		if err != nil {
			panic(err)
		}

		readRtp(path, uint32(ssrc), true, func(data []byte) {
			client.Write(data)
			time.Sleep(1 * time.Millisecond)
		})
	} else {
		addr, _ := net.ResolveTCPAddr(network, localAddr)
		server := transport.TCPServer{}

		server.SetHandler2(func(conn net.Conn) {
			readRtp(path, uint32(ssrc), true, func(data []byte) {
				conn.Write(data)
				time.Sleep(1 * time.Millisecond)
			})
		}, nil, nil)

		err := server.Bind(addr)
		if err != nil {
			panic(err)
		}

		connectSource(id, "192.168.2.148:20001")
		//
	}

	select {}
}
