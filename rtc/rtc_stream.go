package rtc

import (
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/stream"
	"github.com/pion/interceptor"
	"github.com/pion/webrtc/v3"
	"net"
)

var (
	webrtcApi *webrtc.API
)

type transStream struct {
	stream.BaseTransStream
}

func (t *transStream) Input(packet utils.AVPacket) ([][]byte, int64, bool, error) {
	t.ClearOutStreamBuffer()

	if utils.AVMediaTypeAudio == packet.MediaType() {
		t.AppendOutStreamBuffer(packet.Data())
	} else if utils.AVMediaTypeVideo == packet.MediaType() {
		if packet.KeyFrame() {
			extra := t.BaseTransStream.Tracks[packet.Index()].Stream.CodecParameters().AnnexBExtraData()
			t.AppendOutStreamBuffer(extra)
		}

		t.AppendOutStreamBuffer(packet.AnnexBPacketData(t.BaseTransStream.Tracks[packet.Index()].Stream))
	}

	return t.OutBuffer[:t.OutBufferSize], int64(uint32(packet.Duration(1000))), utils.AVMediaTypeVideo == packet.MediaType() && packet.KeyFrame(), nil
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

func TransStreamFactory(source stream.Source, protocol stream.TransStreamProtocol, tracks []*stream.Track) (stream.TransStream, error) {
	return NewTransStream(), nil
}
