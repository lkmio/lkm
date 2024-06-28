package hls

import (
	"fmt"
	"github.com/yangjiechina/avformat/libmpeg"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/lkm/stream"
	"os"
)

type tsContext struct {
	segmentSeq      int    //切片序号
	writeBuffer     []byte //ts流的缓冲区, 由TSMuxer使用. 减少用户态和内核态交互，以及磁盘IO频率
	writeBufferSize int    //已缓存TS流大小

	url  string //m3u8中的url
	path string //位于磁盘中的path

	file *os.File
}

type transStream struct {
	stream.BaseTransStream
	muxer   libmpeg.TSMuxer
	context *tsContext

	m3u8           M3U8Writer
	url            string //m3u8中每个url的前缀
	m3u8Name       string //m3u8文件名
	tsFormat       string //ts文件名格式
	dir            string //m3u8文件父目录
	duration       int    //切片时长, 单位秒
	playlistLength int    //最大切片文件个数
	m3u8File       *os.File

	m3u8Sinks map[stream.SinkId]stream.Sink
}

func (t *transStream) Input(packet utils.AVPacket) error {
	if packet.Index() >= t.muxer.TrackCount() {
		return fmt.Errorf("track not available")
	}

	//创建一下个切片
	//已缓存时长>=指定时长, 如果存在视频, 还需要等遇到关键帧才切片
	if (!t.ExistVideo || utils.AVMediaTypeVideo == packet.MediaType() && packet.KeyFrame()) && float32(t.muxer.Duration())/90000 >= float32(t.duration) {
		//保存当前切片文件
		if t.context.file != nil {
			err := t.flushSegment()
			t.context.segmentSeq++
			if err != nil {
				return err
			}
		}

		//创建新的切片
		if err := t.createSegment(); err != nil {
			return err
		}
	}

	pts := packet.ConvertPts(90000)
	dts := packet.ConvertDts(90000)
	if utils.AVMediaTypeVideo == packet.MediaType() {
		return t.muxer.Input(packet.Index(), packet.AnnexBPacketData(t.BaseTransStream.Tracks[packet.Index()]), pts, dts, packet.KeyFrame())
	} else {
		return t.muxer.Input(packet.Index(), packet.Data(), pts, dts, packet.KeyFrame())
	}
}

func (t *transStream) AddTrack(stream utils.AVStream) error {
	err := t.BaseTransStream.AddTrack(stream)
	if err != nil {
		return err
	}

	if stream.CodecId() == utils.AVCodecIdH264 {
		data := stream.CodecParameters().AnnexBExtraData()
		_, err = t.muxer.AddTrack(stream.Type(), stream.CodecId(), data)
	} else {
		_, err = t.muxer.AddTrack(stream.Type(), stream.CodecId(), stream.Extra())
	}
	return err
}

func (t *transStream) WriteHeader() error {
	return t.createSegment()
}

func (t *transStream) AddSink(sink stream.Sink) error {
	if sink_, ok := sink.(*m3u8Sink); ok {
		if t.m3u8.Size() > 0 {
			return sink.Input([]byte(t.m3u8.ToString()))
		} else {
			t.m3u8Sinks[sink.Id()] = sink_
			return nil
		}
	}

	return t.BaseTransStream.AddSink(sink)
}

func (t *transStream) onTSWrite(data []byte) {
	t.context.writeBufferSize += len(data)
}

func (t *transStream) onTSAlloc(size int) []byte {
	n := len(t.context.writeBuffer) - t.context.writeBufferSize
	if n < size {
		_, _ = t.context.file.Write(t.context.writeBuffer[:t.context.writeBufferSize])
		t.context.writeBufferSize = 0
	}

	return t.context.writeBuffer[t.context.writeBufferSize : t.context.writeBufferSize+size]
}

func (t *transStream) flushSegment() error {
	//将剩余数据写入缓冲区
	if t.context.writeBufferSize > 0 {
		_, _ = t.context.file.Write(t.context.writeBuffer[:t.context.writeBufferSize])
		t.context.writeBufferSize = 0
	}

	if err := t.context.file.Close(); err != nil {
		return err
	}

	//删除多余的ts切片文件
	if t.m3u8.Size() >= t.playlistLength {
		_ = os.Remove(t.m3u8.Head().path)
	}

	//更新m3u8
	duration := float32(t.muxer.Duration()) / 90000
	t.m3u8.AddSegment(duration, t.context.url, t.context.segmentSeq, t.context.path)

	if _, err := t.m3u8File.Seek(0, 0); err != nil {
		return err
	}
	if err := t.m3u8File.Truncate(0); err != nil {
		return err
	}

	m3u8Txt := t.m3u8.ToString()
	if _, err := t.m3u8File.Write([]byte(m3u8Txt)); err != nil {
		return err
	}

	//通知等待m3u8的sink
	if len(t.m3u8Sinks) > 0 {
		for _, sink := range t.m3u8Sinks {
			sink.Input([]byte(m3u8Txt))
		}
		t.m3u8Sinks = make(map[stream.SinkId]stream.Sink, 0)
	}
	return nil
}

// 创建一个新的ts切片
func (t *transStream) createSegment() error {
	tsName := fmt.Sprintf(t.tsFormat, t.context.segmentSeq)
	//ts文件
	t.context.path = fmt.Sprintf("%s/%s", t.dir, tsName)
	//m3u8中的url
	t.context.url = fmt.Sprintf("%s%s", t.url, tsName)

	file, err := os.OpenFile(t.context.path, os.O_WRONLY|os.O_CREATE, 0666)
	if err != nil {
		return err
	}
	t.context.file = file

	t.muxer.Reset()
	err = t.muxer.WriteHeader()
	return err
}

func (t *transStream) Close() error {
	var err error

	if t.muxer != nil {
		t.muxer.Close()
		t.muxer = nil
	}

	if t.context.file != nil {
		err = t.flushSegment()
		err = t.context.file.Close()
		t.context.file = nil
	}

	if t.m3u8File != nil {
		err = t.m3u8File.Close()
		t.m3u8File = nil
	}

	return err
}

// NewTransStream 创建HLS传输流
// @url   		url前缀
// @m3u8Name	m3u8文件名
// @tsFormat	ts文件格式, 例如: %d.ts
// @parentDir	保存切片的绝对路径. mu38和ts切片放在同一目录下, 目录地址使用parentDir+urlPrefix
// @segmentDuration 单个切片时长
// @playlistLength 缓存多少个切片
func NewTransStream(url, m3u8Name, tsFormat, dir string, segmentDuration, playlistLength int) (stream.TransStream, error) {
	//创建文件夹
	if err := os.MkdirAll(dir, 0666); err != nil {
		return nil, err
	}

	//创建m3u8文件
	m3u8Path := fmt.Sprintf("%s/%s", dir, m3u8Name)
	file, err := os.OpenFile(m3u8Path, os.O_CREATE|os.O_RDWR, 0666)
	if err != nil {
		return nil, err
	}

	stream_ := &transStream{
		url:            url,
		m3u8Name:       m3u8Name,
		tsFormat:       tsFormat,
		dir:            dir,
		duration:       segmentDuration,
		playlistLength: playlistLength,
	}

	//创建TS封装器
	muxer := libmpeg.NewTSMuxer()
	muxer.SetWriteHandler(stream_.onTSWrite)
	muxer.SetAllocHandler(stream_.onTSAlloc)

	stream_.context = &tsContext{
		segmentSeq:      0,
		writeBuffer:     make([]byte, 1024*1024),
		writeBufferSize: 0,
	}

	stream_.muxer = muxer
	stream_.m3u8 = NewM3U8Writer(playlistLength)
	stream_.m3u8File = file

	stream_.m3u8Sinks = make(map[stream.SinkId]stream.Sink, 24)
	return stream_, nil
}

func TransStreamFactory(source stream.Source, protocol stream.Protocol, streams []utils.AVStream) (stream.TransStream, error) {
	id := source.Id()
	return NewTransStream("", stream.AppConfig.Hls.M3U8Format(id), stream.AppConfig.Hls.TSFormat(id, "%d"), stream.AppConfig.Hls.Dir, stream.AppConfig.Hls.Duration, stream.AppConfig.Hls.PlaylistLength)
}
