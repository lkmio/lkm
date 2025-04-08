package stream

import (
	"bytes"
	"github.com/lkmio/avformat/collections"
	"math"
	"strconv"
)

const (
	ExtM3u      = "EXTM3U"
	ExtXVersion = "EXT-X-VERSION" //在文件中唯一

	ExtINF              = "EXTINF"              //<duration>(浮点类型, 版本小于3用整型),[<title>]
	ExXByteRange        = "EXT-X-BYTERANGE"     //版本4及以上,分片位置
	ExtXDiscontinuity   = "EXT-X-DISCONTINUITY" //后面的切片不连续, 文件格式、时间戳发生变化
	ExtXKey             = "EXT-X-KEY"           //加密使用
	ExtXMap             = "EXT-X-MAP"           //音视频的元数据
	ExtXProgramDateTime = "EXT-X-PROGRAM-DATE-TIME"
	ExtXDateRange       = "EXT-X-DATERANGE"

	ExtXTargetDuration        = "EXT-X-TARGETDURATION" //切片最大时长, 整型单位秒
	ExtXMediaSequence         = "EXT-X-MEDIA-SEQUENCE" //第一个切片序号
	ExtXDiscontinuitySequence = "EXT-X-DISCONTINUITY-SEQUENCE"
	ExtXEndList               = "EXT-X-ENDLIST"
	ExtXPlaylistType          = "EXT-X-PLAYLIST-TYPE"
	ExtXIFramesOnly           = "EXT-X-I-FRAMES-ONLY"

	ExtXMedia           = "EXT-X-MEDIA"
	ExtXStreamINF       = "EXT-X-STREAM-INF"
	ExtXIFrameStreamINF = "EXT-X-I-FRAME-STREAM-INF"
	ExtXSessionData     = "EXT-X-SESSION-DATA"
	ExtXSessionKey      = "EXT-X-SESSION-KEY"

	ExtXIndependentSegments = "EXT-X-INDEPENDENT-SEGMENTS"
	ExtXStart               = "EXT-X-START"
)

//HttpContent-Type头必须是"application/vnd.apple.mpegurl"或"audio/mpegurl"
//无BOM

type Segment struct {
	Duration float32
	Url      string
	Sequence int
	Path     string
}

type M3U8Writer interface {
	// AddSegment 添加切片
	//@Params  duration 切片时长
	//@Params  url m3u8列表中切片的url
	//@Params  sequence m3u8列表中的切片序号
	//@Params  path 切片位于磁盘中的绝对路径
	AddSegment(duration float32, url string, sequence int, path string)

	String() string

	// Size 返回切片文件数量
	Size() int

	// Get Head 返回指定索引切片文件
	Get(index int) Segment
}

type m3u8Writer struct {
	stringBuffer *bytes.Buffer
	segments     *collections.Queue[Segment]
}

func (m *m3u8Writer) AddSegment(duration float32 /*title string,*/, url string, sequence int, path string) {
	if m.segments.IsFull() {
		m.segments.Pop()
	}

	m.segments.Push(Segment{Duration: duration, Url: url, Sequence: sequence, Path: path})
}

// 返回切片时长最长的值(秒)
func (m *m3u8Writer) targetDuration() int {
	var targetDuration int
	head, tail := m.segments.Data()

	compute := func(playlist []Segment) {
		for _, segment := range playlist {
			// 会影响播放器缓存.
			round := int(math.Ceil(float64(segment.Duration)))
			if round > targetDuration {
				targetDuration = round
			}
		}
	}

	if head != nil {
		compute(head)
	}

	if tail != nil {
		compute(tail)
	}

	return targetDuration
}

func (m *m3u8Writer) String() string {
	// 仅实现简单的播放列表
	head, tail := m.segments.Data()
	if head == nil {
		return ""
	}

	m.stringBuffer.Reset()
	m.stringBuffer.WriteString("#EXTM3U\r\n")
	// 仅实现第三个版本
	m.stringBuffer.WriteString("#EXT-X-VERSION:3\r\n")
	m.stringBuffer.WriteString("#EXT-X-TARGETDURATION:")
	m.stringBuffer.WriteString(strconv.Itoa(m.targetDuration()))
	m.stringBuffer.WriteString("\r\n")
	m.stringBuffer.WriteString("#EXT-X-MEDIA-SEQUENCE:")
	m.stringBuffer.WriteString(strconv.Itoa(head[0].Sequence))
	m.stringBuffer.WriteString("\r\n")

	appendSegments := func(playlist []Segment) {
		for _, segment := range playlist {
			m.stringBuffer.WriteString("#EXTINF:")
			m.stringBuffer.WriteString(strconv.FormatFloat(float64(segment.Duration), 'f', -1, 32))
			m.stringBuffer.WriteString(",\r\n")
			m.stringBuffer.WriteString(segment.Url + "%s") // %s用于替换每个sink的拉流key
			m.stringBuffer.WriteString("\r\n")
		}
	}

	if head != nil {
		appendSegments(head)
	}

	if tail != nil {
		appendSegments(tail)
	}

	return m.stringBuffer.String()
}

func (m *m3u8Writer) Size() int {
	var size int
	head, tail := m.segments.Data()

	if head != nil {
		size += len(head)
	}

	if tail != nil {
		size += len(tail)
	}

	return size
}

func (m *m3u8Writer) Get(index int) Segment {
	head, tail := m.segments.Data()
	if index >= len(head) {
		return tail[index-len(head)]
	} else {
		return head[index]
	}
}

func NewM3U8Writer(len int) M3U8Writer {
	return &m3u8Writer{
		stringBuffer: bytes.NewBuffer(make([]byte, 0, 1024*10)),
		segments:     collections.NewQueue[Segment](len),
	}
}
