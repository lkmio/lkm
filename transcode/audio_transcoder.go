//go:build audio_transcode
// +build audio_transcode

package transcode

import (
	"fmt"
	audio_transcoder "github.com/lkmio/audio-transcoder"
	"github.com/lkmio/avformat"
	"github.com/lkmio/avformat/utils"
)

func init() {
	CreateAudioTranscoder = NewAudioTranscoder
}

type AudioTranscoder struct {
	decoder   audio_transcoder.Decoder
	encoder   audio_transcoder.Encoder
	encoderID utils.AVCodecID
	// reSampler audio_transcoder.Resampler
	pcmData []byte
	pktData []byte
}

func (t *AudioTranscoder) Transcode(src *avformat.AVPacket, cb func([]byte, int)) (int, error) {
	if src.MediaType != utils.AVMediaTypeAudio {
		return 0, fmt.Errorf("unsupported media type: %s", src.MediaType.String())
	}

	pcmN, err := t.decoder.Decode(src.Data, t.pcmData)
	if err != nil {
		return 0, err
	} else if pcmN < 1 {
		return 0, nil
	}

	pktN, err := t.encoder.Encode(t.pcmData[:pcmN], func(bytes []byte) {
		cb(bytes, t.encoder.PacketDurationMS())
	})

	return pktN, nil
}

func (t *AudioTranscoder) Close() {
	t.decoder.Destroy()
	t.encoder.Destroy()
}

func (t *AudioTranscoder) GetEncoderID() utils.AVCodecID {
	return t.encoderID
}

func NewAudioTranscoder(src *avformat.AVStream, dst []utils.AVCodecID) (Transcoder, *avformat.AVStream, error) {
	decoder := audio_transcoder.FindDecoder(src.CodecID.String())
	if decoder == nil {
		return nil, nil, fmt.Errorf("unsupported audio codec: %s", src.CodecID.String())
	}

	var err error
	var encoder audio_transcoder.Encoder
	var dstCodec utils.AVCodecID
	for _, codec := range dst {
		encoder, err = audio_transcoder.FindEncoder(codec.String(), src.SampleRate, src.Channels)
		if encoder != nil {
			dstCodec = codec
			break
		}
	}

	if err != nil {
		return nil, nil, err
	} else if encoder == nil {
		return nil, nil, fmt.Errorf("unsupported audio codec: %s", src.CodecID.String())
	}

	switch src.CodecID {
	case utils.AVCodecIdAAC:
		if err = decoder.(*audio_transcoder.AACDecoder).Create(nil, src.Data); err != nil {
			return nil, nil, err
		}
		break
	case utils.AVCodecIdOPUS:
		if err = decoder.(*audio_transcoder.OpusDecoder).Create(src.SampleRate, src.Channels); err != nil {
			return nil, nil, err
		}
		break
	}

	adtsHeader := 1
	switch dstCodec {
	case utils.AVCodecIdAAC:
		if _, err = encoder.(*audio_transcoder.AACEncoder).Create(src.SampleRate, src.Channels, adtsHeader); err != nil {
			decoder.Destroy()
			return nil, nil, err
		}
		break
	case utils.AVCodecIdOPUS:
		if _, err = encoder.(*audio_transcoder.OpusEncoder).Create(src.SampleRate, src.Channels); err != nil {
			decoder.Destroy()
			return nil, nil, err
		}
	}

	dstStream := &avformat.AVStream{}
	*dstStream = *src
	dstStream.CodecID = dstCodec
	dstStream.Timebase = 1000

	if data := encoder.ExtraData(); data != nil {
		dstStream.Data = make([]byte, len(data))
		copy(dstStream.Data, data)
	}

	if utils.AVCodecIdAAC == dstCodec {
		dstStream.HasADTSHeader = adtsHeader == 1
	}

	return &AudioTranscoder{
		decoder:   decoder,
		encoder:   encoder,
		pcmData:   make([]byte, src.SampleRate*src.Channels*2),
		pktData:   make([]byte, src.SampleRate*src.Channels*2),
		encoderID: dstCodec,
	}, dstStream, nil
}
