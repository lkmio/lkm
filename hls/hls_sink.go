package hls

import (
	"fmt"
	"github.com/lkmio/lkm/log"
	"github.com/lkmio/lkm/stream"
	"strings"
	"time"
)

const (
	SessionIdKey = "hls_sid"
)

type M3U8Sink struct {
	stream.BaseSink
	cb               func(m3u8 []byte) // 生成m3u8文件的发送回调
	sessionId        string
	playtime         time.Time
	playTimer        *time.Timer
	m3u8StringFormat *string
}

// SendM3U8Data 首次向拉流端应答M3U8文件， 后续更新M3U8文件, 通过调用@see GetM3U8String 函数获取最新的M3U8文件.
func (s *M3U8Sink) SendM3U8Data(data *string) error {
	s.m3u8StringFormat = data
	s.cb([]byte(s.GetM3U8String()))

	// 开启计时器, 长时间没有拉流关闭sink
	timeout := time.Duration(stream.AppConfig.IdleTimeout)
	if timeout < time.Second {
		timeout = time.Duration(stream.AppConfig.Hls.Duration) * 2 * 3 * time.Second
	}

	s.playTimer = time.AfterFunc(timeout, func() {
		sub := time.Now().Sub(s.playtime)
		if sub > timeout {
			log.Sugar.Errorf("hls拉流超时 sink: %s ", s.ID)

			s.Close()
			return
		}

		s.playTimer.Reset(timeout)
	})

	return nil
}

func (s *M3U8Sink) StartStreaming(transStream stream.TransStream) error {
	hls := transStream.(*TransStream)

	if hls.m3u8.Size() > 0 {
		if err := s.SendM3U8Data(&hls.m3u8StringFormat); err != nil {
			return err
		}
	} else {
		// m3u8文件中还没有切片时, 将sink添加到等待队列
		hls.m3u8Sinks[s.GetID()] = s
	}

	return nil
}

func (s *M3U8Sink) GetM3U8String() string {
	// 更新拉流时间
	//s.RefreshPlayTime()

	param := fmt.Sprintf("?%s=%s", SessionIdKey, s.sessionId)
	m3u8 := strings.ReplaceAll(*s.m3u8StringFormat, "%s", param)
	return m3u8
}

func (s *M3U8Sink) RefreshPlayTime() {
	s.playtime = time.Now()
}

func (s *M3U8Sink) Close() {
	if s.playTimer != nil {
		s.playTimer.Stop()
		s.playTimer = nil
	}

	stream.SinkManager.Remove(s.ID)
	s.BaseSink.Close()
}

func NewM3U8Sink(id stream.SinkID, sourceId string, cb func(m3u8 []byte), sessionId string) stream.Sink {
	return &M3U8Sink{
		BaseSink:  stream.BaseSink{ID: id, SourceID: sourceId, Protocol: stream.TransStreamHls, TCPStreaming: true},
		cb:        cb,
		sessionId: sessionId,
	}
}
