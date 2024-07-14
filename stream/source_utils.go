package stream

import (
	"encoding/hex"
	"fmt"
	"github.com/lkmio/avformat/libavc"
	"github.com/lkmio/avformat/libhevc"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/log"
	"net/url"
	"strings"
)

func (s SourceType) ToString() string {
	if SourceTypeRtmp == s {
		return "rtmp"
	} else if SourceType28181 == s {
		return "28181"
	} else if SourceType1078 == s {
		return "jt1078"
	}

	panic(fmt.Sprintf("unknown source type %d", s))
}

func (p Protocol) ToString() string {
	if ProtocolRtmp == p {
		return "rtmp"
	} else if ProtocolFlv == p {
		return "flv"
	} else if ProtocolRtsp == p {
		return "rtsp"
	} else if ProtocolHls == p {
		return "hls"
	} else if ProtocolRtc == p {
		return "rtc"
	}

	panic(fmt.Sprintf("unknown stream protocol %d", p))
}

func Path2SourceId(path string, suffix string) (string, error) {
	source := strings.TrimSpace(path)
	if strings.HasPrefix(source, "/") {
		source = source[1:]
	}

	if len(suffix) > 0 && strings.HasSuffix(source, suffix) {
		source = source[:len(source)-len(suffix)]
	}

	source = strings.TrimSpace(source)

	if len(strings.TrimSpace(source)) == 0 {
		return "", fmt.Errorf("the request source cannot be empty")
	}

	return source, nil
}

// ParseUrl 从推拉流url中解析出流id和url参数
func ParseUrl(name string) (string, url.Values) {
	index := strings.Index(name, "?")
	if index > 0 && index < len(name)-1 {
		query, err := url.ParseQuery(name[index+1:])
		if err != nil {
			log.Sugar.Errorf("解析url参数失败 err:%s url:%s", err.Error(), name)
			return name, nil
		}

		return name[:index], query
	}

	return name, nil
}

func ExtractVideoPacket(codec utils.AVCodecID, key, extractStream bool, data []byte, pts, dts int64, index, timebase int) (utils.AVStream, utils.AVPacket, error) {
	var stream utils.AVStream

	if utils.AVCodecIdH264 == codec {
		//从关键帧中解析出sps和pps
		if key && extractStream {
			sps, pps, err := libavc.ParseExtraDataFromKeyNALU(data)
			if err != nil {
				log.Sugar.Errorf("从关键帧中解析sps pps失败 data:%s", hex.EncodeToString(data))
				return nil, nil, err
			}

			codecData, err := utils.NewAVCCodecData(sps, pps)
			if err != nil {
				log.Sugar.Errorf("解析sps pps失败 data:%s sps:%s, pps:%s", hex.EncodeToString(data), hex.EncodeToString(sps), hex.EncodeToString(pps))
				return nil, nil, err
			}

			stream = utils.NewAVStream(utils.AVMediaTypeVideo, 0, codec, codecData.AnnexBExtraData(), codecData)
		}

	} else if utils.AVCodecIdH265 == codec {
		if key && extractStream {
			vps, sps, pps, err := libhevc.ParseExtraDataFromKeyNALU(data)
			if err != nil {
				log.Sugar.Errorf("从关键帧中解析vps sps pps失败  data:%s", hex.EncodeToString(data))
				return nil, nil, err
			}

			codecData, err := utils.NewHEVCCodecData(vps, sps, pps)
			if err != nil {
				log.Sugar.Errorf("解析sps pps失败 data:%s vps:%s sps:%s, pps:%s", hex.EncodeToString(data), hex.EncodeToString(vps), hex.EncodeToString(sps), hex.EncodeToString(pps))
				return nil, nil, err
			}

			stream = utils.NewAVStream(utils.AVMediaTypeVideo, 0, codec, codecData.AnnexBExtraData(), codecData)
		}

	}

	packet := utils.NewVideoPacket(data, dts, pts, key, utils.PacketTypeAnnexB, codec, index, timebase)
	return stream, packet, nil
}

func ExtractAudioPacket(codec utils.AVCodecID, extractStream bool, data []byte, pts, dts int64, index, timebase int) (utils.AVStream, utils.AVPacket, error) {
	var stream utils.AVStream
	var packet utils.AVPacket
	if utils.AVCodecIdAAC == codec {
		//必须包含ADTSHeader
		if len(data) < 7 {
			return nil, nil, fmt.Errorf("need more data")
		}

		var skip int
		header, err := utils.ReadADtsFixedHeader(data)
		if err != nil {
			log.Sugar.Errorf("读取ADTSHeader失败 data:%s", hex.EncodeToString(data[:7]))
			return nil, nil, err
		} else {
			skip = 7
			//跳过ADtsHeader长度
			if header.ProtectionAbsent() == 0 {
				skip += 2
			}
		}

		if extractStream {
			configData, err := utils.ADtsHeader2MpegAudioConfigData(header)
			config, err := utils.ParseMpeg4AudioConfig(configData)
			println(config)
			if err != nil {
				log.Sugar.Errorf("adt头转m4ac失败 data:%s", hex.EncodeToString(data[:7]))
				return nil, nil, err
			}

			stream = utils.NewAVStream(utils.AVMediaTypeAudio, index, codec, configData, nil)
		}

		packet = utils.NewAudioPacket(data[skip:], dts, pts, codec, index, timebase)
	} else if utils.AVCodecIdPCMALAW == codec || utils.AVCodecIdPCMMULAW == codec {
		if extractStream {
			stream = utils.NewAVStream(utils.AVMediaTypeAudio, index, codec, nil, nil)
		}

		packet = utils.NewAudioPacket(data, dts, pts, codec, index, timebase)
	}

	return stream, packet, nil
}
