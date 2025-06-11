package jt1078

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/lkmio/avformat"
	"github.com/lkmio/avformat/bufio"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/gb28181"
	"github.com/lkmio/lkm/stream"
	"github.com/lkmio/mpeg"
	"github.com/lkmio/transport"
	"net"
	"net/http"
	"os"
	"testing"
	"time"
)

type Handler struct {
	muxer   *mpeg.PSMuxer
	fos     *os.File
	buffer  []byte
	tracks  map[int]int
	gateway *gb28181.GBGateway
	udp     *transport.UDPClient
}

func (h Handler) OnNewTrack(track avformat.Track) {
	addTrack, err := h.muxer.AddTrack(track.GetStream().MediaType, track.GetStream().CodecID)
	if err != nil {
		println(err.Error())
	} else {
		h.tracks[track.GetStream().Index] = addTrack
		h.gateway.AddTrack(&stream.Track{Stream: track.GetStream()})
	}
}

func (h Handler) OnTrackComplete() {
}

func (h Handler) OnTrackNotFind() {
	//TODO implement me
	panic("implement me")
}

func (h Handler) OnPacket(packet *avformat.AVPacket) {
	i, ok := h.tracks[packet.Index]
	if !ok {
		return
	}

	dts := packet.ConvertDts(90000)
	pts := packet.ConvertPts(90000)
	var n int
	if packet.MediaType == utils.AVMediaTypeVideo {
		// 1078流已经是annexb打包
		// annexBData := avformat.AVCCPacket2AnnexB(t.BaseTransStream.Tracks[packet.Index].Stream, packet)
		n = h.muxer.Input(h.buffer, i, packet.Key, packet.Data, &pts, &dts)
	} else {
		n = h.muxer.Input(h.buffer, i, true, packet.Data, &pts, &dts)
	}

	if n > 0 {
		h.fos.Write(h.buffer[:n])
	}

	packets, _, _, err := h.gateway.Input(packet, i)
	if err != nil {
		panic(err)
	}

	for _, refPacket := range packets {
		bytes := refPacket.Get()
		err = h.udp.Write(bytes[2:])
		if err != nil {
			panic(err)
		}
	}
}

func publish(path string) {
	client := transport.TCPClient{}
	addr, err := net.ResolveTCPAddr("tcp", "127.0.0.1:1078")
	if err != nil {
		panic(err)
	}
	_, err = client.Connect(nil, addr)
	if err != nil {
		panic(err)
	}

	file, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}

	index := 0
	for index < len(file) {
		n := bufio.MinInt(len(file)-index, 1500)
		client.Write(file[index : index+n])
		index += n
		time.Sleep(1 * time.Millisecond)
	}
}

func TestPublish(t *testing.T) {
	t.Run("decode_1078_data", func(t *testing.T) {
		data, err := os.ReadFile("../dump/jt1078-127.0.0.1.50659")
		if err != nil {
			panic(err)
		}

		delimiter := [4]byte{0x30, 0x31, 0x63, 0x64}
		decoder := transport.NewDelimiterFrameDecoder(1024*1024*2, delimiter[:])

		length := len(data)
		for j := 0; j < length; j += 4 {
			size := int(binary.BigEndian.Uint32(data[j:]))
			if 4+size > length-j {
				break
			}

			rtp := data[j+4 : j+4+size]
			var n int
			for length := len(rtp); n < length; {
				i, bytes, err := decoder.Input(rtp[n:])
				if err != nil {
					panic(err)
				} else if len(bytes) < 1 {
					break
				}

				n += i
				packet := Packet{}
				err = packet.Unmarshal(bytes)
				if err != nil {
					panic(err)
				}

				fmt.Printf("1078 packet ts: %d\r\n", packet.ts)
			}

			j += size
		}

	})

	t.Run("publish", func(t *testing.T) {
		path := "../../source_files/10352264314-2.bin"
		//path := "../../source_files/013800138000-1.bin"
		publish(path)
	})

	// 1078->ps->rtp
	// 1078封装成ps流保存到文件, 再用rtp打包发送出去, 用wireshark导出ps流看播放是否正常
	t.Run("jt2gb", func(t *testing.T) {
		//path := "../../source_files/10352264314-2.bin"
		path := "../../source_files/013800138000-1.bin"
		file, err := os.ReadFile(path)
		if err != nil {
			panic(err)
		}

		openFile, err := os.OpenFile(path+".ps", os.O_CREATE|os.O_RDWR, 0666)
		if err != nil {
			panic(err)
		}

		client := &transport.UDPClient{}
		addr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:10000")
		err = client.Connect(nil, addr)
		if err != nil {
			panic(err)
		}

		demuxer := NewDemuxer()
		demuxer.SetHandler(&Handler{
			muxer:   mpeg.NewPsMuxer(),
			buffer:  make([]byte, 1024*1024*2),
			fos:     openFile,
			tracks:  make(map[int]int),
			gateway: gb28181.NewGBGateway(0xFFFFFFFF),
			udp:     client,
		})

		defer demuxer.Close()

		delimiter := [4]byte{0x30, 0x31, 0x63, 0x64}
		decoder := transport.NewDelimiterFrameDecoder(1024*1024*2, delimiter[:])
		var n int
		for {
			r, bytes, err := decoder.Input(file[n:])
			if err != nil || bytes == nil {
				break
			}

			n += r
			_, err = demuxer.Input(bytes)
			if err != nil {
				panic(err)
			}
		}
	})

	// hook gb-cms的on_invite回调, 处理invite请求, 推送本地文件,发送200响应
	t.Run("hook_on_invite", func(t *testing.T) {
		// 创建http server
		router := mux.NewRouter()

		// 示例路由
		router.HandleFunc("/api/v1/jt1078/on_invite", func(w http.ResponseWriter, r *http.Request) {
			v := struct {
				SimNumber     string `json:"sim_number,omitempty"`
				ChannelNumber string `json:"channel_number,omitempty"`
			}{}

			// 读取请求体
			bytes := make([]byte, 1024)
			n, err := r.Body.Read(bytes)
			if n < 1 {
				panic(err)
			}
			err = json.Unmarshal(bytes[:n], &v)
			if err != nil {
				panic(err)
			}

			fmt.Printf("on_invite sim_number: %s, channel_number: %s\r\n", v.SimNumber, v.ChannelNumber)

			var path string
			if v.SimNumber == "10352264314" {
				path = "../../source_files/10352264314-2.bin"
			} else if v.SimNumber == "13800138000" {
				path = "../../source_files/013800138000-1.bin"
			} else {
				w.WriteHeader(http.StatusBadRequest)
			}

			w.WriteHeader(http.StatusOK)
			go publish(path)
		})

		server := &http.Server{
			Addr:         "localhost:8081",
			Handler:      router,
			WriteTimeout: 15 * time.Second,
			ReadTimeout:  15 * time.Second,
		}

		err := server.ListenAndServe()
		if err != nil {
			panic(err)
		}
	})
}
