package rtc

import (
	"fmt"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/log"
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

func (t *transStream) Input(packet utils.AVPacket) error {
	for _, iSink := range t.Sinks {
		sink_ := iSink.(*sink)
		if sink_.state < webrtc.ICEConnectionStateConnected {
			continue
		}

		if utils.AVMediaTypeAudio == packet.MediaType() {
			sink_.input(packet.Index(), packet.Data(), uint32(packet.Duration(1000)))
		} else if utils.AVMediaTypeVideo == packet.MediaType() {
			if packet.KeyFrame() {
				extra := t.BaseTransStream.Tracks[packet.Index()].CodecParameters().AnnexBExtraData()
				sink_.input(packet.Index(), extra, 0)
			}

			sink_.input(packet.Index(), packet.AnnexBPacketData(t.BaseTransStream.Tracks[packet.Index()]), uint32(packet.Duration(1000)))
		}
	}

	return nil
}

func (t *transStream) AddSink(sink_ stream.Sink) error {
	//创建PeerConnection
	var videoTrack *webrtc.TrackLocalStaticSample
	rtcSink := sink_.(*sink)
	rtcSink.setTrackCount(len(t.Tracks))
	connection, err := webrtcApi.NewPeerConnection(webrtc.Configuration{})
	connection.OnICECandidate(func(candidate *webrtc.ICECandidate) {

	})

	for index, track := range t.Tracks {
		var mimeType string
		var id string
		if utils.AVCodecIdH264 == track.CodecId() {
			mimeType = webrtc.MimeTypeH264
		} else if utils.AVCodecIdH265 == track.CodecId() {
			mimeType = webrtc.MimeTypeH265
		} else if utils.AVCodecIdAV1 == track.CodecId() {
			mimeType = webrtc.MimeTypeAV1
		} else if utils.AVCodecIdVP8 == track.CodecId() {
			mimeType = webrtc.MimeTypeVP8
		} else if utils.AVCodecIdVP9 == track.CodecId() {
			mimeType = webrtc.MimeTypeVP9
		} else if utils.AVCodecIdOPUS == track.CodecId() {
			mimeType = webrtc.MimeTypeOpus
		} else if utils.AVCodecIdPCMALAW == track.CodecId() {
			mimeType = webrtc.MimeTypePCMA
		} else if utils.AVCodecIdPCMMULAW == track.CodecId() {
			mimeType = webrtc.MimeTypePCMU
		} else {
			log.Sugar.Errorf("codec %d not compatible with webrtc", track.CodecId())
			continue
		}

		if utils.AVMediaTypeAudio == track.Type() {
			id = "audio"
		} else {
			id = "video"
		}

		videoTrack, err = webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: mimeType}, id, "pion")
		if err != nil {
			panic(err)
		} else if _, err := connection.AddTransceiverFromTrack(videoTrack, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly}); err != nil {
			return err
		} else if _, err = connection.AddTrack(videoTrack); err != nil {
			return err
		}

		rtcSink.addTrack(index, videoTrack)
	}

	if len(connection.GetTransceivers()) == 0 {
		return fmt.Errorf("no track added")
	} else if err = connection.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: rtcSink.offer}); err != nil {
		return err
	}

	complete := webrtc.GatheringCompletePromise(connection)
	answer, err := connection.CreateAnswer(nil)
	if err != nil {
		return err
	} else if err = connection.SetLocalDescription(answer); err != nil {
		return err
	}

	<-complete
	connection.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		rtcSink.state = state
		log.Sugar.Infof("ice state:%v sink:%d source:%s", state.String(), rtcSink.GetID(), rtcSink.SourceID)

		if state > webrtc.ICEConnectionStateDisconnected {
			log.Sugar.Errorf("webrtc peer断开连接 sink:%v source:%s", rtcSink.GetID(), rtcSink.SourceID)
			rtcSink.Close()
		}
	})

	rtcSink.peer = connection
	rtcSink.SendHeader([]byte(connection.LocalDescription().SDP))
	return t.BaseTransStream.AddSink(sink_)
}

func (t *transStream) WriteHeader() error {
	return nil
}

func NewTransStream() stream.TransStream {
	t := &transStream{}
	return t
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

	//设置公网ip和监听端口
	setting.SetICEUDPMux(webrtc.NewICEUDPMux(nil, udpListener))
	setting.SetNAT1To1IPs(ips, webrtc.ICECandidateTypeHost)

	//注册音视频编码器
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

func TransStreamFactory(source stream.Source, protocol stream.TransStreamProtocol, streams []utils.AVStream) (stream.TransStream, error) {
	return NewTransStream(), nil
}
