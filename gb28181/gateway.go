package gb28181

import (
	"encoding/binary"
	"github.com/lkmio/avformat"
	"github.com/lkmio/avformat/collections"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/stream"
	"github.com/lkmio/mpeg"
	"github.com/lkmio/rtp"
)

type GBGateway struct {
	stream.BaseTransStream
	ps        *mpeg.PSMuxer
	rtp       rtp.Muxer
	psBuffer  []byte
	rtpBuffer *stream.RtpBuffer
}

func (s *GBGateway) AddTrack(track *stream.Track) (int, error) {
	index, err := s.ps.AddTrack(track.Stream.MediaType, track.Stream.CodecID)
	if err != nil {
		return -1, err
	}

	return index, nil
}

func (s *GBGateway) Input(packet *avformat.AVPacket, index int) ([]*collections.ReferenceCounter[[]byte], int64, bool, error) {
	dts := packet.ConvertDts(90000)
	pts := packet.ConvertPts(90000)

	data := packet.Data
	if utils.AVMediaTypeVideo == packet.MediaType {
		data = avformat.AVCCPacket2AnnexB(s.FindTrackWithStreamIndex(packet.Index).Stream, packet)
	}

	// 扩容ps buffer
	if cap(s.psBuffer) < len(data)+1024*64 {
		s.psBuffer = make([]byte, len(data)*2)
	}

	n := s.ps.Input(s.psBuffer, index, packet.Key, data, &pts, &dts)

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

func (s *GBGateway) Close() ([]stream.TransStreamSegment, error) {
	s.rtpBuffer.Clear()
	return nil, nil
}

func NewGBGateway(ssrc uint32) *GBGateway {
	return &GBGateway{
		ps:        mpeg.NewPsMuxer(),
		rtp:       rtp.NewMuxer(96, 0, ssrc),
		psBuffer:  make([]byte, 1024*1024*2),
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
