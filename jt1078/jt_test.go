package jt1078

import (
	"encoding/binary"
	"fmt"
	"github.com/lkmio/avformat/bufio"
	"github.com/lkmio/transport"
	"net"
	"os"
	"testing"
	"time"
)

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

		//path := "../../source_files/10352264314-2.bin"
		path := "../../source_files/013800138000-1.bin"

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
	})
}
