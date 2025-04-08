package stream

import (
	"github.com/lkmio/avformat/utils"
)

type TrackManager struct {
	tracks []*Track
}

func (s *TrackManager) Add(track *Track) {
	for _, t := range s.tracks {
		utils.Assert(t.Stream.MediaType != track.Stream.MediaType)
		utils.Assert(t.Stream.CodecID != track.Stream.CodecID)
	}

	s.tracks = append(s.tracks, track)
}

func (s *TrackManager) Find(id utils.AVCodecID) *Track {
	for _, track := range s.tracks {
		if track.Stream.CodecID == id {
			return track
		}
	}

	return nil
}

func (s *TrackManager) FindWithType(mediaType utils.AVMediaType) *Track {
	for _, track := range s.tracks {
		if track.Stream.MediaType == mediaType {
			return track
		}
	}

	return nil
}

func (s *TrackManager) FindTracks(id utils.AVCodecID) []*Track {
	var tracks []*Track
	for _, track := range s.tracks {
		if track.Stream.CodecID == id {
			tracks = append(tracks, track)
		}
	}

	return tracks
}

func (s *TrackManager) FindTracksWithType(mediaType utils.AVMediaType) []*Track {
	var tracks []*Track
	for _, track := range s.tracks {
		if track.Stream.MediaType == mediaType {
			tracks = append(tracks, track)
		}
	}

	return tracks
}

func (s *TrackManager) All() []*Track {
	return s.tracks
}
