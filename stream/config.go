package stream

import (
	"strings"
)

const (
	DefaultMergeWriteLatency = 350
)

type TransportConfig struct {
	Transport string    //"UDP|TCP"
	Port      [2]uint16 //单端口-1个元素/多端口-2个元素
}

type RtmpConfig struct {
	Enable bool   `json:"enable"`
	Addr   string `json:"addr"`
}

type RtspConfig struct {
	TransportConfig

	Addr     string
	Enable   bool   `json:"enable"`
	Password string `json:"password"`
}

type RecordConfig struct {
	Enable bool   `json:"enable"`
	Format string `json:"format"`
}

type HlsConfig struct {
	Enable         bool   `json:"enable"`
	Dir            string `json:"dir"`
	Duration       int    `json:"duration"`
	PlaylistLength int    `json:"playlist_length"`
}

type LogConfig struct {
	Level     int
	Name      string
	MaxSize   int
	MaxBackup int
	MaxAge    int
	Compress  bool
}

type HttpConfig struct {
	Enable bool   `json:"enable"`
	Addr   string `json:"addr"`
}

type GB28181Config struct {
	TransportConfig
	Addr string `json:"addr"`
}

type JT1078Config struct {
	Enable bool   `json:"enable"`
	Addr   string `json:"addr"`
}

func (g TransportConfig) EnableTCP() bool {
	return strings.Contains(g.Transport, "TCP")
}

func (g TransportConfig) EnableUDP() bool {
	return strings.Contains(g.Transport, "UDP")
}

func (g TransportConfig) IsMultiPort() bool {
	return g.Port[1] > 0 && g.Port[1] > g.Port[0]
}

// M3U8Path 根据sourceId返回m3u8的磁盘路径
// 切片及目录生成规则, 以SourceId为34020000001320000001/34020000001320000001为例:
// 创建文件夹34020000001320000001, 34020000001320000001.m3u8文件, 文件列表中切片url为34020000001320000001_seq.ts
func (c HlsConfig) M3U8Path(sourceId string) string {
	return c.Dir + "/" + sourceId + ".m3u8"
}

// M3U8Dir 根据id返回m3u8文件位于磁盘中的绝对目录
func (c HlsConfig) M3U8Dir(sourceId string) string {
	split := strings.Split(sourceId, "/")
	return AppConfig.Hls.Dir + "/" + strings.Join(split[:len(split)-1], "/")
}

// M3U8Format 根据id返回m3u8文件名
func (c HlsConfig) M3U8Format(sourceId string) string {
	split := strings.Split(sourceId, "/")
	return split[len(split)-1] + ".m3u8"
}

// TSPath 根据sourceId和ts文件名返回ts的磁盘绝对路径
func (c HlsConfig) TSPath(sourceId string, tsSeq string) string {
	return c.Dir + "/" + sourceId + "_" + tsSeq + ".ts"
}

// TSFormat 根据id返回ts文件名
func (c HlsConfig) TSFormat(sourceId string) string {
	split := strings.Split(sourceId, "/")
	return split[len(split)-1] + "_%d.ts"
}

type HookConfig struct {
	Enable              bool   `json:"enable"`
	Timeout             int64  `json:"timeout"`
	OnPublishUrl        string `json:"on_publish"`         //推流回调
	OnPublishDoneUrl    string `json:"on_publish_done"`    //推流结束回调
	OnPlayUrl           string `json:"on_play"`            //拉流回调
	OnPlayDoneUrl       string `json:"on_play_done"`       //拉流结束回调
	OnRecordUrl         string `json:"on_record"`          //录制流回调
	OnIdleTimeoutUrl    string `json:"on_idle_timeout"`    //没有sink拉流回调
	OnReceiveTimeoutUrl string `json:"on_receive_timeout"` //没有推流回调
}

func (hook *HookConfig) EnablePublishEvent() bool {
	return hook.Enable && hook.OnPublishUrl != ""
}

func (hook *HookConfig) EnableOnPublishDone() bool {
	return hook.Enable && hook.OnPublishDoneUrl != ""
}

func (hook *HookConfig) EnableOnPlay() bool {
	return hook.Enable && hook.OnPlayUrl != ""
}

func (hook *HookConfig) EnableOnPlayDone() bool {
	return hook.Enable && hook.OnPlayDoneUrl != ""
}

func (hook *HookConfig) EnableOnRecord() bool {
	return hook.Enable && hook.OnRecordUrl != ""
}

func (hook *HookConfig) EnableOnIdleTimeout() bool {
	return hook.Enable && hook.OnIdleTimeoutUrl != ""
}

func (hook *HookConfig) EnableOnReceiveTimeout() bool {
	return hook.Enable && hook.OnReceiveTimeoutUrl != ""
}

var AppConfig AppConfig_

func init() {
	AppConfig = AppConfig_{}
}

// AppConfig_ GOP缓存和合并写必须保持一致，同时开启或关闭. 关闭GOP缓存，是为了降低延迟，很难理解又另外开启合并写.
type AppConfig_ struct {
	GOPCache       bool   `json:"gop_cache"`       //是否开启GOP缓存，只缓存一组音视频
	GOPBufferSize  int    `json:"gop_buffer_size"` //预估GOPBuffer大小, AVPacket缓存池和合并写缓存池都会参考此大小
	ProbeTimeout   int    `json:"probe_timeout"`
	PublicIP       string `json:"public_ip"`
	IdleTimeout    int64  `json:"idle_timeout"`    //多长时间没有拉流, 单位秒. 如果开启hook通知, 根据hook响应, 决定是否关闭Source(200-不关闭/非200关闭). 否则会直接关闭Source.
	ReceiveTimeout int64  `json:"receive_timeout"` //多长时间没有收到流, 单位秒. 如果开启hook通知, 根据hook响应, 决定是否关闭Source(200-不关闭/非200关闭). 否则会直接关闭Source.

	//缓存指定时长的包，满了之后才发送给Sink. 可以降低用户态和内核态的交互频率，大幅提升性能.
	//合并写的大小范围，应当大于一帧的时长，不超过一组GOP的时长，在实际发送流的时候也会遵循此条例.
	MergeWriteLatency int `json:"mw_latency"`
	Rtmp              RtmpConfig
	Rtsp              RtspConfig
	Hook              HookConfig
	Record            RecordConfig
	Hls               HlsConfig
	Log               LogConfig
	Http              HttpConfig
	GB28181           GB28181Config
	JT1078            JT1078Config
}
