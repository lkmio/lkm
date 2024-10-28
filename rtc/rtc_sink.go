package rtc

import (
	"fmt"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/log"
	"github.com/lkmio/lkm/stream"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
	"time"
)

type Sink struct {
	stream.BaseSink

	offer  string
	answer string

	peer   *webrtc.PeerConnection
	tracks []*webrtc.TrackLocalStaticSample
	state  webrtc.ICEConnectionState

	cb func(sdp string)
}

func (s *Sink) StartStreaming(transStream stream.TransStream) error {
	// 创建PeerConnection
	var videoTrack *webrtc.TrackLocalStaticSample
	s.setTrackCount(transStream.TrackCount())

	connection, err := webrtcApi.NewPeerConnection(webrtc.Configuration{})
	connection.OnICECandidate(func(candidate *webrtc.ICECandidate) {

	})

	tracks := transStream.GetTracks()
	for index, track := range tracks {
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
			log.Sugar.Errorf("codec %s not compatible with webrtc", track.CodecId())
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

		s.addTrack(index, videoTrack)
	}

	if len(connection.GetTransceivers()) == 0 {
		return fmt.Errorf("no track added")
	} else if err = connection.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: s.offer}); err != nil {
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
		s.state = state
		log.Sugar.Infof("ice state:%v sink:%d source:%s", state.String(), s.GetID(), s.SourceID)

		if state > webrtc.ICEConnectionStateDisconnected {
			log.Sugar.Errorf("webrtc peer断开连接 sink: %v source :%s", s.GetID(), s.SourceID)
			s.Close()
		}
	})

	s.peer = connection
	// offer的sdp, 应答给http请求
	s.cb(connection.LocalDescription().SDP)
	return nil
}
func (s *Sink) setTrackCount(count int) {
	s.tracks = make([]*webrtc.TrackLocalStaticSample, count)
}

func (s *Sink) addTrack(index int, track *webrtc.TrackLocalStaticSample) error {
	s.tracks[index] = track
	return nil
}

func (s *Sink) Write(index int, data [][]byte, ts int64) error {
	if s.tracks[index] == nil {
		return nil
	}

	for _, bytes := range data {
		err := s.tracks[index].WriteSample(media.Sample{
			Data:     bytes,
			Duration: time.Duration(ts) * time.Millisecond,
		})

		if err != nil {
			return err
		}
	}

	return nil
}

func NewSink(id stream.SinkID, sourceId string, offer string, cb func(sdp string)) stream.Sink {
	return &Sink{stream.BaseSink{ID: id, SourceID: sourceId, Protocol: stream.TransStreamRtc, TCPStreaming: false}, offer, "", nil, nil, webrtc.ICEConnectionStateNew, cb}
}
