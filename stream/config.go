package stream

import "strings"

const (
	DefaultMergeWriteLatency = 350
)

type RtmpConfig struct {
	Enable bool   `json:"enable"`
	Addr   string `json:"addr"`
}

type RtspConfig struct {
	RtmpConfig
	Password string
	Port     [2]uint16
}

type RecordConfig struct {
	Enable bool   `json:"enable"`
	Format string `json:"format"`
}

type HlsConfig struct {
	Enable         bool
	Dir            string
	Duration       int
	PlaylistLength int
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
	Enable bool
	Addr   string
}

type GB28181Config struct {
	Addr      string
	Transport string    //"UDP|TCP"
	Port      [2]uint16 //单端口模式[0]=port/多端口模式[0]=start port, [0]=end port.
}

type JT1078Config struct {
	Enable bool
	Addr   string
}

func (g GB28181Config) EnableTCP() bool {
	return strings.Contains(g.Transport, "TCP")
}

func (g GB28181Config) EnableUDP() bool {
	return strings.Contains(g.Transport, "UDP")
}

func (g GB28181Config) IsMultiPort() bool {
	return g.Port[1] > 0 && g.Port[1] > g.Port[0]
}

// M3U8Path 根据sourceId返回m3u8的磁盘路径
func (c HlsConfig) M3U8Path(sourceId string) string {
	return c.Dir + "/" + c.M3U8Format(sourceId)
}

func (c HlsConfig) M3U8Format(sourceId string) string {
	return sourceId + ".m3u8"
}

// TSPath 根据sourceId和ts文件名返回ts的磁盘路径
func (c HlsConfig) TSPath(sourceId string, tsSeq string) string {
	return c.Dir + "/" + c.TSFormat(sourceId, tsSeq)
}

func (c HlsConfig) TSFormat(sourceId string, tsSeq string) string {
	return sourceId + "_" + tsSeq + ".ts"
}

type HookConfig struct {
	Time          int
	Enable        bool   `json:"enable"`
	OnPublish     string `json:"on_publish"`      //推流回调
	OnPublishDone string `json:"on_publish_done"` //推流结束回调
	OnPlay        string `json:"on_play"`         //拉流回调
	OnPlayDone    string `json:"on_play_done"`    //拉流结束回调
	OnRecord      string `json:"on_record"`       //录制流回调
	OnIdleTimeout string `json:"on_idle_timeout"` //多久没有sink拉流回调
	OnRecvTimeout string `json:"on_recv_timeout"` //多久没有推流回调
}

func (hook *HookConfig) EnableOnPublish() bool {
	return hook.OnPublish != ""
}

func (hook *HookConfig) EnableOnPublishDone() bool {
	return hook.OnPublishDone != ""
}

func (hook *HookConfig) EnableOnPlay() bool {
	return hook.OnPlay != ""
}

func (hook *HookConfig) EnableOnPlayDone() bool {
	return hook.OnPlayDone != ""
}

func (hook *HookConfig) EnableOnRecord() bool {
	return hook.OnRecord != ""
}

func (hook *HookConfig) EnableOnIdleTimeout() bool {
	return hook.OnIdleTimeout != ""
}

func (hook *HookConfig) EnableOnRecvTimeout() bool {
	return hook.OnRecvTimeout != ""
}

var AppConfig AppConfig_

func init() {
	AppConfig = AppConfig_{}
}

// AppConfig_ GOP缓存和合并写必须保持一致，同时开启或关闭. 关闭GOP缓存，是为了降低延迟，很难理解又另外开启合并写.
type AppConfig_ struct {
	GOPCache     bool `json:"gop_cache"` //是否开启GOP缓存，只缓存一组音视频
	ProbeTimeout int  `json:"probe_timeout"`

	//缓存指定时长的包，满了之后才发送给Sink. 可以降低用户态和内核态的交互频率，大幅提升性能.
	//合并写的大小范围，应当大于一帧的时长，不超过一组GOP的时长，在实际发送流的时候也会遵循此条例.
	MergeWriteLatency int `json:"mw_latency"`
	Rtmp              RtmpConfig
	Rtsp              RtmpConfig

	Hook HookConfig

	Record RecordConfig
	Hls    HlsConfig

	Log LogConfig

	Http HttpConfig

	GB28181 GB28181Config

	JT1078 JT1078Config
}
