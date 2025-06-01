package hls

import (
	"context"
	"fmt"
	"github.com/lkmio/avformat/collections"
	"github.com/lkmio/lkm/stream"
	"strings"
	"sync/atomic"
	"time"
)

const (
	SessionIDKey = "hls_sid"
)

type M3U8Sink struct {
	stream.BaseSink
	playingTime     atomic.Value
	sessionId       string // 拉流会话ID
	playlistFormat  *string
	m3u8ReadyCtx    context.Context
	m3u8ReadyCancel func()
}

func (s *M3U8Sink) Write(index int, data []*collections.ReferenceCounter[[]byte], ts int64, keyVideo bool) error {
	if s.playlistFormat == nil {
		s.playlistFormat = bytesToStringPtr(data[0].Get())
		s.m3u8ReadyCancel()
	}

	return nil
}

func (s *M3U8Sink) GetPlaylist(ctx context.Context) string {
	// 更新拉流时间
	s.RefreshPlayingTime()

	if s.playlistFormat == nil {
		if ctx == nil {
			return ""
		}

		select {
		case <-ctx.Done():
			return ""
		case <-s.m3u8ReadyCtx.Done():
			if s.playlistFormat == nil {
				return ""
			}
		}
	}

	// 替换每个sink唯一的拉流会话ID
	param := fmt.Sprintf("?%s=%s", SessionIDKey, s.sessionId)
	playlist := strings.ReplaceAll(*s.playlistFormat, "%s", param)
	return playlist
}

func (s *M3U8Sink) RefreshPlayingTime() {
	s.playingTime.Store(time.Now())
}

func (s *M3U8Sink) GetPlayingTime() time.Time {
	if t := s.playingTime.Load(); t != nil {
		return t.(time.Time)
	}

	return time.Time{}
}

func (s *M3U8Sink) Close() {
	s.m3u8ReadyCancel()
	s.BaseSink.Close()
	SinkManager.Remove(s.ID)
}

func NewM3U8Sink(id stream.SinkID, sourceId string, sessionId string) stream.Sink {
	ctx, cancel := context.WithCancel(context.Background())
	return &M3U8Sink{
		BaseSink:        stream.BaseSink{ID: id, SourceID: sourceId, Protocol: stream.TransStreamHls, TCPStreaming: true},
		sessionId:       sessionId,
		m3u8ReadyCtx:    ctx,
		m3u8ReadyCancel: cancel,
	}
}
