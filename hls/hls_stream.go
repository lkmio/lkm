package hls

import (
	"fmt"
	"github.com/yangjiechina/avformat/libmpeg"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/live-server/stream"
	"os"
)

type tsContext struct {
	segmentSeq      int
	writeBuffer     []byte
	writeBufferSize int

	url  string
	path string

	file *os.File
}

type Stream struct {
	stream.TransStreamImpl
	muxer   libmpeg.TSMuxer
	context *tsContext

	m3u8     M3U8Writer
	url      string
	m3u8Name string
	tsFormat string
	dir      string
	duration int
	m3u8File *os.File
}

// NewTransStream 创建HLS传输流
// @url   		url前缀
// @m3u8Name	m3u8文件名
// @tsFormat	ts文件格式, 例如: test_%d.ts
// @parentDir	保存切片的绝对路径. mu38和ts切片放在同一目录下, 目录地址使用parentDir+urlPrefix
// @segmentDuration 单个切片时长
// @playlistLength 缓存多少个切片
func NewTransStream(url, m3u8Name, tsFormat, dir string, segmentDuration, playlistLength int) (stream.ITransStream, error) {
	//创建文件夹
	if err := os.MkdirAll(dir, 0666); err != nil {
		return nil, err
	}

	m3u8Path := fmt.Sprintf("%s/%s", dir, m3u8Name)
	file, err := os.OpenFile(m3u8Path, os.O_CREATE|os.O_RDWR, 0666)
	if err != nil {
		return nil, err
	}

	stream_ := &Stream{
		url:      url,
		m3u8Name: m3u8Name,
		tsFormat: tsFormat,
		dir:      dir,
		duration: segmentDuration,
	}

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
	return stream_, nil
}

func (t *Stream) Input(packet utils.AVPacket) error {
	if packet.Index() >= t.muxer.TrackCount() {
		return fmt.Errorf("track not available")
	}

	//创建一下个切片
	if (!t.ExistVideo || utils.AVMediaTypeVideo == packet.MediaType() && packet.KeyFrame()) && float32(t.muxer.Duration())/90000 >= float32(t.duration) {
		if err := t.createSegment(); err != nil {
			return err
		}
	}

	if utils.AVMediaTypeVideo == packet.MediaType() {
		return t.muxer.Input(packet.Index(), packet.AnnexBPacketData(), packet.Pts()*90, packet.Dts()*90, packet.KeyFrame())
	} else {
		return t.muxer.Input(packet.Index(), packet.Data(), packet.Pts()*90, packet.Dts()*90, packet.KeyFrame())
	}
}

func (t *Stream) AddTrack(stream utils.AVStream) error {
	err := t.TransStreamImpl.AddTrack(stream)
	if err != nil {
		return err
	}

	if stream.CodecId() == utils.AVCodecIdH264 {
		data, err := stream.AnnexBExtraData()
		if err != nil {
			return err
		}

		_, err = t.muxer.AddTrack(stream.Type(), stream.CodecId(), data)
	} else {
		_, err = t.muxer.AddTrack(stream.Type(), stream.CodecId(), stream.Extra())
	}
	return err
}

func (t *Stream) WriteHeader() error {
	return t.createSegment()
}

func (t *Stream) onTSWrite(data []byte) {
	t.context.writeBufferSize += len(data)
}

func (t *Stream) onTSAlloc(size int) []byte {
	n := len(t.context.writeBuffer) - t.context.writeBufferSize
	if n < size {
		_, _ = t.context.file.Write(t.context.writeBuffer[:t.context.writeBufferSize])
		t.context.writeBufferSize = 0
	}

	return t.context.writeBuffer[t.context.writeBufferSize : t.context.writeBufferSize+size]
}

func (t *Stream) flushSegment() error {
	//将剩余数据写入缓冲区
	if t.context.writeBufferSize > 0 {
		_, _ = t.context.file.Write(t.context.writeBuffer[:t.context.writeBufferSize])
		t.context.writeBufferSize = 0
	}

	if err := t.context.file.Close(); err != nil {
		return err
	}

	duration := float32(t.muxer.Duration()) / 90000
	t.m3u8.AddSegment(duration, t.context.url, t.context.segmentSeq)

	//更新m3u8
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

	return nil
}

func (t *Stream) createSegment() error {
	if t.context.file != nil {
		err := t.flushSegment()
		t.context.segmentSeq++
		if err != nil {
			return err
		}
	}

	tsName := fmt.Sprintf(t.tsFormat, t.context.segmentSeq)
	t.context.path = fmt.Sprintf("%s%s", t.dir, tsName)
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

func (t *Stream) Close() error {
	var err error

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
