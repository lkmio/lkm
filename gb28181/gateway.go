package gb28181

import (
	"encoding/binary"
	"fmt"
	"github.com/lkmio/avformat"
	"github.com/lkmio/avformat/collections"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/log"
	"github.com/lkmio/lkm/stream"
	"github.com/lkmio/mpeg"
	"github.com/lkmio/rtp"
)

type GBGateway struct {
	stream.BaseTransStream
	ps        *mpeg.PSMuxer
	rtp       rtp.Muxer
	psBuffer  []byte
	tracks    map[utils.AVCodecID]int // codec->track index
	rtpBuffer *stream.RtpBuffer
}

func (s *GBGateway) WriteHeader() error {
	if len(s.tracks) == 0 {
		return fmt.Errorf("no tracks available")
	}
	return nil
}

func (s *GBGateway) AddTrack(track *stream.Track) error {
	s.BaseTransStream.AddTrack(track)

	if utils.AVCodecIdH264 == track.Stream.CodecID || utils.AVCodecIdH265 == track.Stream.CodecID || utils.AVCodecIdAAC == track.Stream.CodecID || utils.AVCodecIdPCMALAW == track.Stream.CodecID || utils.AVCodecIdPCMMULAW == track.Stream.CodecID {
	} else {
		log.Sugar.Errorf("不支持的编码格式: %d", track.Stream.CodecID)
		return nil
	}

	index, err := s.ps.AddTrack(track.Stream.MediaType, track.Stream.CodecID)
	if err != nil {
		log.Sugar.Error("添加%s到ps muxer失败", track.Stream.CodecID)
		return nil
	}

	s.tracks[track.Stream.CodecID] = index
	return nil
}

func (s *GBGateway) Input(packet *avformat.AVPacket) ([]*collections.ReferenceCounter[[]byte], int64, bool, error) {
	trackIndex, ok := s.tracks[packet.CodecID]
	if !ok {
		log.Sugar.Errorf("未找到对应的track: %d", packet.CodecID)
		return nil, 0, false, nil
	}

	dts := packet.ConvertDts(90000)
	pts := packet.ConvertPts(90000)

	data := packet.Data
	if utils.AVMediaTypeVideo == packet.MediaType {
		data = avformat.AVCCPacket2AnnexB(s.BaseTransStream.Tracks[packet.Index].Stream, packet)
	}

	// 扩容ps buffer
	if cap(s.psBuffer) < len(data)+1024*64 {
		s.psBuffer = make([]byte, len(data)*2)
	}

	n := s.ps.Input(s.psBuffer, trackIndex, packet.Key, data, &pts, &dts)

	var result []*collections.ReferenceCounter[[]byte]
	var rtpBuffer []byte
	var counter *collections.ReferenceCounter[[]byte]
	s.rtp.Input(s.psBuffer[:n], uint32(dts), func() []byte {
		counter = s.rtpBuffer.Get()
		counter.Refer()
		rtpBuffer = counter.Get()
		return rtpBuffer[2:]
	}, func(bytes []byte) {
		binary.BigEndian.PutUint16(rtpBuffer, uint16(len(bytes)))
		counter.ResetData(rtpBuffer[:2+len(bytes)])
		result = append(result, counter)
	})

	// 引用计数保持为1
	for _, pkt := range result {
		pkt.Release()
	}

	return result, 0, true, nil
}

func (s *GBGateway) Close() ([]*collections.ReferenceCounter[[]byte], int64, error) {
	s.rtpBuffer.Clear()
	return nil, 0, nil
}

func NewGBGateway(ssrc uint32) *GBGateway {
	return &GBGateway{
		ps:        mpeg.NewPsMuxer(),
		rtp:       rtp.NewMuxer(96, 0, ssrc),
		psBuffer:  make([]byte, 1024*1024*2),
		tracks:    make(map[utils.AVCodecID]int),
		rtpBuffer: stream.NewRtpBuffer(1024),
	}
}

func GatewayTransStreamFactory(source stream.Source, _ stream.TransStreamProtocol, _ []*stream.Track, sink stream.Sink) (stream.TransStream, error) {
	// 默认ssrc
	var ssrc uint32 = 0xFFFFFFFF

	// 优先使用sink的ssrc, 减少内存拷贝
	if sink != nil {
		if forwardSink, ok := sink.(*stream.ForwardSink); ok {
			ssrc = forwardSink.GetSSRC()
		}
	}

	gateway := NewGBGateway(ssrc)
	return gateway, nil
}
