package gb28181

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/lkmio/avformat"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/mpeg"
	"github.com/lkmio/transport"
	"github.com/pion/rtp"
	"io"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"testing"
	"time"
)

func connectSource(source string, addr string) {
	v := &struct {
		Source     string `json:"source"` //GetSourceID
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

func createSource(source, setup string, ssrc uint32) (string, uint16, uint32) {
	v := struct {
		Source      string `json:"source"` //GetSourceID
		Setup       string `json:"setup"`  //active/passive
		SSRC        string `json:"ssrc,omitempty"`
		SessionName string `json:"session_name,omitempty"` // play/download/playback/talk/broadcast
	}{
		Source:      source,
		Setup:       setup,
		SSRC:        strconv.Itoa(int(ssrc)),
		SessionName: "play",
	}

	marshal, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}

	//request, err := http.NewRequest("POST", "http://localhost:8080/api/v1/gb28181/source/create", bytes.NewBuffer(marshal))
	request, err := http.NewRequest("POST", "http://localhost:8080/api/v1/gb28181/offer/create", bytes.NewBuffer(marshal))
	if err != nil {
		panic(err)
	}

	client := http.Client{}
	response, err := client.Do(request)
	if err != nil {
		panic(err)
	} else if response.StatusCode != http.StatusOK {
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
			Addr string `json:"addr"`
			SSRC string `json:"ssrc,omitempty"`
		}
	}{}

	err = json.Unmarshal(all, connectInfo)
	if err != nil {
		panic(err)
	}

	atoi, err := strconv.Atoi(connectInfo.Data.SSRC)
	if err != nil {
		panic(err)
	}

	host, p, err := net.SplitHostPort(connectInfo.Data.Addr)
	Port, err := strconv.Atoi(p)
	return host, uint16(Port), uint32(atoi)
}

// 分割rtp包, 返回rtp over tcp包
func splitPackets(data []byte, ssrc uint32) ([][]byte, uint32) {
	tcp := binary.BigEndian.Uint16(data) <= 1500
	length := len(data)
	var packets [][]byte
	if tcp {
		var offset int
		for i := 0; i < length; i += 2 {
			if i > 0 {
				packets = append(packets, data[offset:i])
			}

			offset = i
			i += int(binary.BigEndian.Uint16(data[i:]))
		}

		if len(packets) > 0 {
			packet := rtp.Packet{}
			err := packet.Unmarshal(packets[0][2:])
			if err != nil {
				panic(err)
			}

			return packets, packet.SSRC
		}
	} else {
		// udp包根据ssrc查找
		var offset int
		for i := 0; i < length-4; i++ {
			if ssrc != binary.BigEndian.Uint32(data[i:]) {
				continue
			}

			if i-8 != 0 {
				packet := data[offset : i-8]
				bytes := make([]byte, 2+len(packet))
				binary.BigEndian.PutUint16(bytes, uint16(len(packet)))
				copy(bytes[2:], packet)
				packets = append(packets, bytes)
			}

			offset = i - 8
		}

		return packets, ssrc
	}

	return nil, ssrc
}

var ts int64 = -1

func ctrDelay(data []byte) {
	packet := rtp.Packet{}
	err := packet.Unmarshal(data)
	if err != nil {
		panic(err)
	}

	if ts == -1 {
		ts = int64(packet.Timestamp)
	}

	if dis := (int64(packet.Timestamp) - ts) / 90; dis > 0 {
		time.Sleep(time.Duration(dis) * time.Millisecond)
	}

	ts = int64(packet.Timestamp)
}

func modifySSRC(data []byte, ssrc uint32) {
	packet := rtp.Packet{}
	err := packet.Unmarshal(data)
	if err != nil {
		panic(err)
	}

	packet.SSRC = ssrc
	bytes, err := packet.Marshal()
	utils.Assert(len(bytes) == len(data))
	copy(data, bytes)
}

// 使用wireshark直接导出的rtp流
// 根据ssrc来查找每个rtp包, rtp不要带扩展字段
func TestPublish(t *testing.T) {
	path := "../../source_files/gb28181_h264.rtp"
	var rawSsrc uint32 = 0xBEBC201
	localAddr := "0.0.0.0:20001"
	id := "hls_mystream"

	data, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}

	var packets [][]byte
	packets, rawSsrc = splitPackets(data, rawSsrc)
	utils.Assert(len(packets) > 0)

	sort.Slice(packets, func(i, j int) bool {
		packet := rtp.Packet{}
		if err := packet.Unmarshal(packets[i][2:]); err != nil {
			panic(err)
		}
		packet2 := rtp.Packet{}
		if err := packet2.Unmarshal(packets[j][2:]); err != nil {
			panic(err)
		}

		return packet.SequenceNumber < packet2.SequenceNumber
	})

	t.Run("demux", func(t *testing.T) {
		buffer := mpeg.NewProbeBuffer(1024 * 1024 * 2)
		demuxer := mpeg.NewPSDemuxer(true)
		demuxer.SetHandler(&avformat.OnUnpackStream2FileHandler{
			Path: "./ps_demux",
		})

		file, err := os.OpenFile("./ps_demux.ps", os.O_WRONLY|os.O_CREATE, 132)
		if err != nil {
			panic(err)
		}

		for _, packet := range packets {
			file.Write(packet[14:])
			bytes, err := buffer.Input(packet[14:])
			if err != nil {
				panic(err)
			}

			n, err := demuxer.Input(bytes)
			if err != nil {
				panic(err)
			}

			buffer.Reset(n)
		}
	})

	t.Run("udp", func(t *testing.T) {
		ip, port, ssrc := createSource(id, "udp", rawSsrc)

		addr, _ := net.ResolveUDPAddr("udp", localAddr)
		remoteAddr, _ := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", ip, port))

		client := &transport.UDPClient{}
		err := client.Connect(addr, remoteAddr)
		if err != nil {
			panic(err)
		}

		for _, packet := range packets {
			modifySSRC(packet[2:], ssrc)
			client.Write(packet[2:])
			ctrDelay(packet[2:])
		}
	})

	t.Run("passive", func(t *testing.T) {
		ip, port, ssrc := createSource(id, "passive", rawSsrc)

		addr, _ := net.ResolveTCPAddr("tcp", localAddr)
		remoteAddr, _ := net.ResolveTCPAddr("tcp", fmt.Sprintf("%s:%d", ip, port))

		client := transport.TCPClient{}
		_, err := client.Connect(addr, remoteAddr)

		if err != nil {
			panic(err)
		}

		for _, packet := range packets {
			modifySSRC(packet[2:], ssrc)
			client.Write(packet)
			ctrDelay(packet[2:])
		}
	})

	t.Run("active", func(t *testing.T) {
		ip, port, ssrc := createSource(id, "active", rawSsrc)

		addr, _ := net.ResolveTCPAddr("tcp", localAddr)
		server := transport.TCPServer{}

		server.SetHandler2(func(conn net.Conn) []byte {
			for _, packet := range packets {
				modifySSRC(packet[2:], ssrc)
				conn.Write(packet)
				ctrDelay(packet[2:])
			}

			return nil
		}, nil, nil)

		err := server.Bind(addr)
		if err != nil {
			panic(err)
		}

		connectSource(id, fmt.Sprintf("%s:%d", ip, port))
	})
}
