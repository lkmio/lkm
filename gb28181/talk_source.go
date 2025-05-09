package gb28181

import (
	"github.com/gorilla/websocket"
	"github.com/lkmio/avformat"
	"github.com/lkmio/avformat/bufio"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/stream"
	"time"
)

type Demuxer struct {
	avformat.BaseDemuxer
	ts            int64
	firstOfPacket bool
}

func (d *Demuxer) Input(data []byte) (int, error) {
	length := len(data)

	if !d.firstOfPacket {
		d.firstOfPacket = true
		d.OnNewAudioTrack(0, utils.AVCodecIdPCMALAW, 8000, nil, avformat.AudioConfig{
			HasADTSHeader: false,
			Channels:      1,
			SampleRate:    8000,
			SampleSize:    2,
		})

		d.ProbeComplete()
	}

	for i := 0; i < length; {
		n := bufio.MinInt(length-i, 320)
		_, _ = d.DataPipeline.Write(data[i:i+n], 0, utils.AVMediaTypeAudio)
		pkt, _ := d.DataPipeline.Feat(0)
		d.OnAudioPacket(0, utils.AVCodecIdPCMALAW, pkt, d.ts)
		d.ts += int64(n)
		i += n
	}

	return length, nil
}

type TalkSource struct {
	stream.PublishSource
}

func (s *TalkSource) Input(data []byte) error {
	_, err := s.PublishSource.TransDemuxer.Input(data)
	return err
}

func (s *TalkSource) Close() {
	s.PublishSource.Close()
	// 关闭所有对讲设备的会话
	stream.CloseWaitingSinks(s.ID)
}

type WSConn struct {
	*websocket.Conn
}

func (w WSConn) Read(b []byte) (n int, err error) {
	panic("implement me")
}

func (w WSConn) Write(block []byte) (n int, err error) {
	panic("implement me")
}

func (w WSConn) SetDeadline(t time.Time) error {
	panic("implement me")
}

func NewTalkSource(id string, conn *websocket.Conn) *TalkSource {
	s := &TalkSource{
		PublishSource: stream.PublishSource{
			ID:   id,
			Type: stream.SourceTypeGBTalk,
			Conn: &WSConn{conn},
			TransDemuxer: &Demuxer{
				BaseDemuxer: avformat.BaseDemuxer{
					DataPipeline: &avformat.StreamsBuffer{},
					Name:         "gb_talk",
					AutoFree:     false,
				},
			},
		},
	}

	s.TransDemuxer.SetHandler(s)
	return s
}
