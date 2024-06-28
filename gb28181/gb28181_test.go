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

func createSource(source, setup string, ssrc uint32) (string, uint16) {
	v := struct {
		Source string `json:"source"` //SourceId
		Setup  string `json:"setup"`  //active/passive
		SSRC   uint32 `json:"ssrc,omitempty"`
	}{
		Source: source,
		Setup:  setup,
		SSRC:   ssrc,
	}

	marshal, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}

	request, err := http.NewRequest("POST", "http://localhost:8080/api/v1/gb28181/source/create", bytes.NewBuffer(marshal))
	if err != nil {
		panic(err)
	}

	client := http.Client{}
	response, err := client.Do(request)
	if err != nil {
		panic(err)
	}
	if response.StatusCode != http.StatusOK {
		panic("")
	}

	all, err := io.ReadAll(response.Body)
	if err != nil {
		panic(err)
	}

	connectInfo := &struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			IP   string `json:"ip"`
			Port uint16 `json:"port,omitempty"`
		}
	}{}

	err = json.Unmarshal(all, connectInfo)
	if err != nil {
		panic(err)
	}

	return connectInfo.Data.IP, connectInfo.Data.Port
}

// 使用wireshark直接导出udp流
// 根据ssrc来查找每个rtp包, rtp不要带扩展字段
func TestUDPRecv(t *testing.T) {
	path := "D:\\GOProjects\\avformat\\gb28181_h265.rtp"
	ssrc := 0xBEBC202
	localAddr := "0.0.0.0:20001"
	setup := "udp" //udp/passive/active
	id := "hls_mystream"

	ip, port := createSource(id, setup, uint32(ssrc))

	if setup == "udp" {
		addr, _ := net.ResolveUDPAddr("udp", localAddr)
		remoteAddr, _ := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", ip, port))

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
		addr, _ := net.ResolveTCPAddr("tcp", localAddr)
		remoteAddr, _ := net.ResolveTCPAddr("tcp", fmt.Sprintf("%s:%d", ip, port))

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
		addr, _ := net.ResolveTCPAddr("tcp", localAddr)
		server := transport.TCPServer{}

		server.SetHandler2(func(conn net.Conn) []byte {
			readRtp(path, uint32(ssrc), true, func(data []byte) {
				conn.Write(data)
				time.Sleep(1 * time.Millisecond)
			})

			return nil
		}, nil, nil)

		err := server.Bind(addr)
		if err != nil {
			panic(err)
		}

		connectSource(id, fmt.Sprintf("%s:%d", ip, port))
	}

	select {}
}
