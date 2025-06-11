package rtc

import (
	"github.com/lkmio/avformat"
	"github.com/lkmio/avformat/collections"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/stream"
	"github.com/pion/interceptor"
	"github.com/pion/webrtc/v3"
	"net"
)

var (
	webrtcApi *webrtc.API

	SupportedCodecs = map[utils.AVCodecID]interface{}{
		utils.AVCodecIdH264:      webrtc.MimeTypeH264,
		utils.AVCodecIdH265:      webrtc.MimeTypeH265,
		utils.AVCodecIdAV1:       webrtc.MimeTypeAV1,
		utils.AVCodecIdVP8:       webrtc.MimeTypeVP8,
		utils.AVCodecIdVP9:       webrtc.MimeTypeVP9,
		utils.AVCodecIdOPUS:      webrtc.MimeTypeOpus,
		utils.AVCodecIdPCMALAW:   webrtc.MimeTypePCMA,
		utils.AVCodecIdPCMMULAW:  webrtc.MimeTypePCMU,
		utils.AVCodecIdADPCMG722: webrtc.MimeTypeG722,
	}
)

type transStream struct {
	stream.BaseTransStream
}

func (t *transStream) Input(packet *avformat.AVPacket, _ int) ([]*collections.ReferenceCounter[[]byte], int64, bool, error) {
	t.ClearOutStreamBuffer()

	if utils.AVMediaTypeAudio == packet.MediaType {
		t.AppendOutStreamBuffer(collections.NewReferenceCounter(packet.Data))
	} else if utils.AVMediaTypeVideo == packet.MediaType {
		avStream := t.FindTrackWithStreamIndex(packet.Index).Stream
		if packet.Key {
			extra := avStream.CodecParameters.AnnexBExtraData()
			t.AppendOutStreamBuffer(collections.NewReferenceCounter(extra))
		}

		data := avformat.AVCCPacket2AnnexB(avStream, packet)
		t.AppendOutStreamBuffer(collections.NewReferenceCounter(data))
	}

	return t.OutBuffer[:t.OutBufferSize], int64(uint32(packet.GetDuration(1000))), utils.AVMediaTypeVideo == packet.MediaType && packet.Key, nil
}

func (t *transStream) WriteHeader() error {
	return nil
}

func InitConfig() {
	setting := webrtc.SettingEngine{}
	var ips []string
	ips = append(ips, stream.AppConfig.PublicIP)

	udpListener, err := net.ListenUDP("udp", &net.UDPAddr{
		IP:   net.ParseIP(stream.AppConfig.ListenIP),
		Port: stream.AppConfig.WebRtc.Port,
	})

	if err != nil {
		panic(err)
	}

	// 设置公网ip和监听端口
	setting.SetICEUDPMux(webrtc.NewICEUDPMux(nil, udpListener))
	setting.SetNAT1To1IPs(ips, webrtc.ICECandidateTypeHost)

	// 注册音视频编码器
	m := &webrtc.MediaEngine{}
	if err := m.RegisterDefaultCodecs(); err != nil {
		panic(err)
	}

	i := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(m, i); err != nil {
		panic(err)
	}

	webrtcApi = webrtc.NewAPI(webrtc.WithMediaEngine(m), webrtc.WithInterceptorRegistry(i), webrtc.WithSettingEngine(setting))
}

func NewTransStream() stream.TransStream {
	t := &transStream{}
	return t
}

func TransStreamFactory(_ stream.Source, _ stream.TransStreamProtocol, _ []*stream.Track, _ stream.Sink) (stream.TransStream, error) {
	return NewTransStream(), nil
}
