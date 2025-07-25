package main

import (
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/flv"
	"github.com/lkmio/lkm/hls"
	"github.com/lkmio/lkm/rtsp"
	"github.com/lkmio/lkm/stream"
)

// 处理不同包不能相互引用的需求

func NewStreamEndInfo(source string, originTracks []*stream.Track, streams map[stream.TransStreamID]stream.TransStream) *stream.StreamEndInfo {
	if len(streams) < 1 {
		return nil
	}

	info := stream.StreamEndInfo{
		ID:           source,
		Timestamps:   make(map[stream.TransStreamID]map[utils.AVCodecID][2]int64, len(streams)),
		OriginTracks: make(map[utils.AVCodecID]interface{}, len(originTracks)),
	}

	for _, transStream := range streams {
		// 获取ts切片序号
		if stream.TransStreamHls == transStream.GetProtocol() {
			if hls := transStream.(*hls.TransStream); hls.M3U8Writer.Size() > 0 {
				info.M3U8Writer = hls.M3U8Writer
				info.PlaylistFormat = hls.PlaylistFormat
			}
		} else if stream.TransStreamRtsp == transStream.GetProtocol() {
			if rtsp := transStream.(*rtsp.TransStream); len(rtsp.Tracks) > 0 {
				info.RtspTracks = make(map[utils.AVCodecID]uint16, 8)
				for _, track := range rtsp.RtspTracks {
					info.RtspTracks[track.CodecID] = track.EndSeq
				}
			}
		} else if stream.TransStreamFlv == transStream.GetProtocol() {
			flv := transStream.(*flv.TransStream)
			info.FLVPrevTagSize = flv.Muxer.PrevTagSize()
		}

		// 保存传输流最后的时间戳
		tracks := transStream.GetTracks()
		ts := make(map[utils.AVCodecID][2]int64, len(tracks))
		for _, track := range tracks {
			ts[track.Stream.CodecID] = [2]int64{track.Dts, track.Pts}
		}

		info.Timestamps[transStream.GetID()] = ts
	}

	for _, track := range originTracks {
		info.OriginTracks[track.Stream.CodecID] = nil
	}

	return &info
}
