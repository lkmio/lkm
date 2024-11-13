package stream

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/lkmio/lkm/log"
	"go.uber.org/zap/zapcore"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultMergeWriteLatency = 350
)

type TransportConfig struct {
	Transport string `json:"transport"` //"UDP|TCP"
}

type EnableConfig interface {
	IsEnable() bool

	SetEnable(bool)
}

type enableConfig struct {
	Enable bool `json:"enable"`
}

func (e *enableConfig) IsEnable() bool {
	return e.Enable
}

func (e *enableConfig) SetEnable(b bool) {
	e.Enable = b
}

type PortConfig interface {
	GetPort() int

	SetPort(port int)
}

type portConfig struct {
	Port int `json:"port"`
}

func (s *portConfig) GetPort() int {
	return s.Port
}

func (s *portConfig) SetPort(port int) {
	s.Port = port
}

type RtmpConfig struct {
	enableConfig
	portConfig
}

type HlsConfig struct {
	enableConfig
	Dir            string `json:"dir"`
	Duration       int    `json:"segment_duration"`
	PlaylistLength int    `json:"playlist_length"`
}

type JT1078Config struct {
	enableConfig
	portConfig
}

type RtspConfig struct {
	TransportConfig

	enableConfig
	Port     []int  `json:"port"`
	Password string `json:"password"`
}

type RecordConfig struct {
	enableConfig
	Format string `json:"format"`
	Dir    string `json:"dir"`
}

type LogConfig struct {
	FileLogging bool   `json:"file_logging"`
	Level       int    `json:"level"`
	Name        string `json:"name"`
	MaxSize     int    `json:"max_size"` //单位M
	MaxBackup   int    `json:"max_backup"`
	MaxAge      int    `json:"max_age"` //天数
	Compress    bool   `json:"compress"`
}

type HttpConfig struct {
	Port int `json:"port"`
}

type GB28181Config struct {
	enableConfig
	TransportConfig
	Port []int `json:"port"`
}

type WebRtcConfig struct {
	enableConfig
	TransportConfig
	portConfig
}

func (g TransportConfig) IsEnableTCP() bool {
	return strings.Contains(g.Transport, "TCP")
}

func (g TransportConfig) IsEnableUDP() bool {
	return strings.Contains(g.Transport, "UDP")
}

func (g GB28181Config) IsMultiPort() bool {
	return len(g.Port) > 1
}

func (g RtspConfig) IsMultiPort() bool {
	return len(g.Port) == 3
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

type HooksConfig struct {
	enableConfig
	Timeout             int64  `json:"timeout"`
	OnStartedUrl        string `json:"on_started"`         //应用启动后回调
	OnPublishUrl        string `json:"on_publish"`         //推流回调
	OnPublishDoneUrl    string `json:"on_publish_done"`    //推流结束回调
	OnPlayUrl           string `json:"on_play"`            //拉流回调
	OnPlayDoneUrl       string `json:"on_play_done"`       //拉流结束回调
	OnRecordUrl         string `json:"on_record"`          //录制流回调
	OnIdleTimeoutUrl    string `json:"on_idle_timeout"`    //没有sink拉流回调
	OnReceiveTimeoutUrl string `json:"on_receive_timeout"` //没有推流回调
}

func (hook *HooksConfig) IsEnablePublishEvent() bool {
	return hook.Enable && hook.OnPublishUrl != ""
}

func (hook *HooksConfig) IsEnableOnPublishDone() bool {
	return hook.Enable && hook.OnPublishDoneUrl != ""
}

func (hook *HooksConfig) IsEnableOnPlay() bool {
	return hook.Enable && hook.OnPlayUrl != ""
}

func (hook *HooksConfig) IsEnableOnPlayDone() bool {
	return hook.Enable && hook.OnPlayDoneUrl != ""
}

func (hook *HooksConfig) IsEnableOnRecord() bool {
	return hook.Enable && hook.OnRecordUrl != ""
}

func (hook *HooksConfig) IsEnableOnIdleTimeout() bool {
	return hook.Enable && hook.OnIdleTimeoutUrl != ""
}

func (hook *HooksConfig) IsEnableOnReceiveTimeout() bool {
	return hook.Enable && hook.OnReceiveTimeoutUrl != ""
}

func (hook *HooksConfig) IsEnableOnStarted() bool {
	return hook.Enable && hook.OnStartedUrl != ""
}

func GetStreamPlayUrls(source string) []string {
	var urls []string
	if AppConfig.Rtmp.Enable {
		urls = append(urls, fmt.Sprintf("rtmp://%s:%d/%s", AppConfig.PublicIP, AppConfig.Rtmp.Port, source))
	}

	if AppConfig.Rtsp.Enable {
		// 不拼接userinfo
		urls = append(urls, fmt.Sprintf("rtsp://%s:%d/%s", AppConfig.PublicIP, AppConfig.Rtsp.Port[0], source))
	}

	//if AppConfig.Http.Enable {
	//	return
	//}

	if AppConfig.Hls.Enable {
		urls = append(urls, fmt.Sprintf("http://%s:%d/%s.m3u8", AppConfig.PublicIP, AppConfig.Http.Port, source))
	}

	urls = append(urls, fmt.Sprintf("http://%s:%d/%s.flv", AppConfig.PublicIP, AppConfig.Http.Port, source))
	urls = append(urls, fmt.Sprintf("http://%s:%d/%s.rtc", AppConfig.PublicIP, AppConfig.Http.Port, source))
	urls = append(urls, fmt.Sprintf("ws://%s:%d/%s.flv", AppConfig.PublicIP, AppConfig.Http.Port, source))
	return urls
}

// DumpStream2File 保存推流到文件, 用4字节帧长分割
func DumpStream2File(sourceType SourceType, conn net.Conn, data []byte) {
	if err := os.MkdirAll("dump", 0666); err != nil {
		log.Sugar.Errorf("创建dump文件夹失败 err:%s", err.Error())
		return
	}

	path := fmt.Sprintf("dump/%s-%s", sourceType.String(), conn.RemoteAddr().String())
	path = strings.ReplaceAll(path, ":", ".")

	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0666)
	if err != nil {
		log.Sugar.Errorf("打开dump文件夹失败 err:%s path:%s", err.Error(), path)
		return
	}

	defer file.Close()
	bytes := make([]byte, 4)
	binary.BigEndian.PutUint32(bytes, uint32(len(data)))
	file.Write(bytes)
	file.Write(data)
}

func JoinHostPort(host string, port int) string {
	return net.JoinHostPort(host, strconv.Itoa(port))
}

func ListenAddr(port int) string {
	return JoinHostPort(AppConfig.ListenIP, port)
}

var AppConfig AppConfig_

func init() {
	AppConfig = AppConfig_{}
}

// AppConfig_ GOP缓存和合并写必须保持一致，同时开启或关闭. 关闭GOP缓存，是为了降低延迟，很难理解又另外开启合并写.
type AppConfig_ struct {
	GOPCache            bool   `json:"gop_cache"`       // 是否开启GOP缓存，只缓存一组音视频
	GOPBufferSize       int    `json:"gop_buffer_size"` // 预估GOPBuffer大小, AVPacket缓存池和合并写缓存池都会参考此大小
	ProbeTimeout        int    `json:"probe_timeout"`   // 收流解析AVStream的超时时间
	WriteTimeout        int    `json:"write_timeout"`   // Server向TCP拉流Conn发包的超时时间, 超过该时间, 直接主动断开Conn. 客户端重新拉流的成本小于服务器缓存成本.
	WriteBufferCapacity int    `json:"-"`               // 发送缓冲区容量大小, 缓冲区由多个内存块构成.
	PublicIP            string `json:"public_ip"`
	ListenIP            string `json:"listen_ip"`
	IdleTimeout         int64  `json:"idle_timeout"`    // 多长时间(单位秒)没有拉流. 如果开启hook通知, 根据hook响应, 决定是否关闭Source(200-不关闭/非200关闭). 否则会直接关闭Source.
	ReceiveTimeout      int64  `json:"receive_timeout"` // 多长时间(单位秒)没有收到流. 如果开启hook通知, 根据hook响应, 决定是否关闭Source(200-不关闭/非200关闭). 否则会直接关闭Source.
	Debug               bool   `json:"debug"`           // debug模式, 开启将保存推流

	//缓存指定时长的包，满了之后才发送给Sink. 可以降低用户态和内核态的交互频率，大幅提升性能.
	//合并写的大小范围，应当大于一帧的时长，不超过一组GOP的时长，在实际发送流的时候也会遵循此条例.
	MergeWriteLatency int `json:"mw_latency"`
	Rtmp              RtmpConfig
	Hls               HlsConfig
	JT1078            JT1078Config
	Rtsp              RtspConfig
	GB28181           GB28181Config
	WebRtc            WebRtcConfig

	Hooks  HooksConfig
	Record RecordConfig
	Log    LogConfig
	Http   HttpConfig
}

func LoadConfigFile(path string) (*AppConfig_, error) {
	file, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	config := AppConfig_{}
	if err := json.Unmarshal(file, &config); err != nil {
		return nil, err
	}

	return &config, err
}

func SetDefaultConfig(config *AppConfig_) {
	if !config.GOPCache {
		config.GOPCache = true
		config.GOPBufferSize = 8196 * 1024
		config.MergeWriteLatency = 350
		log.Sugar.Warnf("强制开启GOP缓存")
	}

	config.GOPBufferSize = limitInt(4096*1024/8, 2048*1024*10, config.GOPBufferSize) // 最低4M码率 最高160M码率
	config.MergeWriteLatency = limitInt(350, 2000, config.MergeWriteLatency)         // 最低缓存350毫秒数据才发送 最高缓存2秒数据才发送
	config.ProbeTimeout = limitInt(2000, 5000, config.MergeWriteLatency)             // 2-5秒内必须解析完AVStream
	config.WriteTimeout = limitInt(2000, 10000, config.WriteTimeout)
	config.WriteBufferCapacity = config.WriteTimeout/config.MergeWriteLatency + 1

	config.Log.Level = limitInt(int(zapcore.DebugLevel), int(zapcore.FatalLevel), config.Log.Level)
	config.Log.MaxSize = limitMin(1, config.Log.MaxSize)
	config.Log.MaxBackup = limitMin(1, config.Log.MaxBackup)
	config.Log.MaxAge = limitMin(1, config.Log.MaxAge)

	config.IdleTimeout *= int64(time.Second)
	config.ReceiveTimeout *= int64(time.Second)
	config.Hooks.Timeout *= int64(time.Second)
}

func limitMin(min, value int) int {
	if value < min {
		return min
	}
	return value
}

func limitInt(min, max, value int) int {
	if value < min {
		return min
	} else if value > max {
		return max
	}

	return value
}
