package rtc

import (
	"github.com/lkmio/lkm/stream"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
	"time"
)

type sink struct {
	stream.BaseSink

	offer  string
	answer string

	peer   *webrtc.PeerConnection
	tracks []*webrtc.TrackLocalStaticSample
	state  webrtc.ICEConnectionState

	cb func(sdp string)
}

func NewSink(id stream.SinkId, sourceId string, offer string, cb func(sdp string)) stream.Sink {
	return &sink{stream.BaseSink{Id_: id, SourceId_: sourceId, Protocol_: stream.ProtocolRtc}, offer, "", nil, nil, webrtc.ICEConnectionStateNew, cb}
}

func (s *sink) setTrackCount(count int) {
	s.tracks = make([]*webrtc.TrackLocalStaticSample, count)
}

func (s *sink) addTrack(index int, track *webrtc.TrackLocalStaticSample) error {
	s.tracks[index] = track
	return nil
}

func (s *sink) SendHeader(data []byte) error {
	s.cb(string(data))
	return nil
}

func (s *sink) input(index int, data []byte, ts uint32) error {
	if s.tracks[index] == nil {
		return nil
	}

	return s.tracks[index].WriteSample(media.Sample{
		Data:     data,
		Duration: time.Duration(ts) * time.Millisecond,
	})
}
