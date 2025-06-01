package jt1078

import (
	"encoding/binary"
	"fmt"
	"github.com/lkmio/avformat/utils"
	"strings"
)

const (
	VideoIFrameMark      = 0b000
	VideoPFrameMark      = 0b001
	VideoBFrameMark      = 0b010
	AudioFrameMark       = 0b011
	TransmissionDataMark = 0b100

	PTVideoH264 = 98
	PTVideoH265 = 99
	PTVideoAVS  = 100
	PTVideoSVAC = 101

	PTAudioG711A  = 6
	PTAudioG711U  = 7
	PTAudioG726   = 8
	PTAudioG729A  = 9
	PTAudioAAC    = 19
	PTAudioMP3    = 25
	PTAudioADPCMA = 26
)

// Packet 1078-2016音视频和透传包. 视频帧包头26个字节, 音频帧22个字节, 透传数据14个字节.
type Packet struct {
	pt            byte
	packetType    byte
	ts            uint64
	subMark       byte
	simNumber     string
	channelNumber byte
	payload       []byte
}

func (p *Packet) Unmarshal(data []byte) error {
	length := len(data)
	if length < 12 {
		return fmt.Errorf("invaild data")
	}

	packetType := data[11] >> 4 & 0x0F
	if packetType < AudioFrameMark {
		if length < 26 {
			return fmt.Errorf("invaild data")
		}
	} else if AudioFrameMark == packetType {
		if length < 22 {
			return fmt.Errorf("invaild data")
		}
	} else if TransmissionDataMark == packetType {
		if length < 14 {
			return fmt.Errorf("invaild data")
		}
	} else {
		return fmt.Errorf("unknown packet type %x", packetType)
	}

	// x扩展位,固定为0
	_ = data[0] >> 4 & 0x1
	pt := data[1] & 0x7F
	// seq
	_ = binary.BigEndian.Uint16(data[2:])

	var simNumber string
	for i := 4; i < 10; i++ {
		simNumber += fmt.Sprintf("%02x", data[i])
	}

	simNumber = strings.TrimLeft(simNumber, "0")
	// channel
	channelNumber := data[10]
	// subMark
	subMark := data[11] & 0x0F
	// 时间戳,单位ms
	var ts uint64
	n := 12
	// 音视频帧才有时间戳字段
	if TransmissionDataMark != packetType {
		ts = binary.BigEndian.Uint64(data[n:])
		n += 8
	}

	// 与上一帧的间隔时间戳, 视频帧才有此字段
	if AudioFrameMark > packetType {
		// iFrameInterval
		_ = binary.BigEndian.Uint16(data[n:])
		n += 2
		// lastFrameInterval
		_ = binary.BigEndian.Uint16(data[n:])
		n += 2
	}

	// size
	_ = binary.BigEndian.Uint16(data[n:])
	n += 2

	p.pt = pt
	p.packetType = packetType
	p.ts = ts
	p.simNumber = simNumber
	p.channelNumber = channelNumber
	p.subMark = subMark
	p.payload = data[n:]

	return nil
}

func PT2CodecID(pt byte) (error, utils.AVCodecID) {
	switch pt {
	case PTVideoH264:
		return nil, utils.AVCodecIdH264
	case PTVideoH265:
		return nil, utils.AVCodecIdH265
	case PTAudioG711A:
		return nil, utils.AVCodecIdPCMALAW
	case PTAudioG711U:
		return nil, utils.AVCodecIdPCMMULAW
	case PTAudioAAC:
		return nil, utils.AVCodecIdAAC
	//case PTAudioADPCMA:
	//	return nil, utils.AVCodecIdADPCMAFC
	default:
		return fmt.Errorf("the codec %d is not implemented", pt), utils.AVCodecIdNONE
	}
}
