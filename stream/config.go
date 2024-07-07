package stream

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/yangjiechina/lkm/log"
	"go.uber.org/zap/zapcore"
	"net"
	"os"
	"strconv"
	"strings"
)

const (
	DefaultMergeWriteLatency = 350
)

type TransportConfig struct {
	Transport string `json:"transport"` //"UDP|TCP"
}

type RtmpConfig struct {
	Enable bool `json:"enable"`
	Port   int  `json:"port"`
}

type HlsConfig struct {
	Enable         bool   `json:"enable"`
	Dir            string `json:"dir"`
	Duration       int    `json:"segment_duration"`
	PlaylistLength int    `json:"playlist_length"`
}

type JT1078Config struct {
	Enable bool `json:"enable"`
	Port   int  `json:"port"`
}

type RtspConfig struct {
	TransportConfig

	Enable   bool   `json:"enable"`
	Port     []int  `json:"port"`
	Password string `json:"password"`
}

type RecordConfig struct {
	Enable bool   `json:"enable"`
	Format string `json:"format"`
}

type LogConfig struct {
	Level     int    `json:"level"`
	Name      string `json:"name"`
	MaxSize   int    `json:"max_size"` //单位M
	MaxBackup int    `json:"max_backup"`
	MaxAge    int    `json:"max_age"` //天数
	Compress  bool   `json:"compress"`
}

type HttpConfig struct {
	Port int `json:"port"`
}

type GB28181Config struct {
	TransportConfig
	Port []int `json:"port"`
}

func (g TransportConfig) EnableTCP() bool {
	return strings.Contains(g.Transport, "TCP")
}

func (g TransportConfig) EnableUDP() bool {
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

func GetStreamPlayUrls(sourceId string) []string {
	var urls []string
	if AppConfig.Rtmp.Enable {
		urls = append(urls, fmt.Sprintf("rtmp://%s:%d/%s", AppConfig.PublicIP, AppConfig.Rtmp.Port, sourceId))
	}

	if AppConfig.Rtsp.Enable {
		//不拼接userinfo
		urls = append(urls, fmt.Sprintf("rtsp://%s:%d/%s", AppConfig.PublicIP, AppConfig.Rtsp.Port[0], sourceId))
	}

	//if AppConfig.Http.Enable {
	//	return
	//}

	if AppConfig.Hls.Enable {
		//不拼接userinfo
		urls = append(urls, fmt.Sprintf("http://%s:%d/%s.m3u8", AppConfig.PublicIP, AppConfig.Http.Port, sourceId))
	}

	urls = append(urls, fmt.Sprintf("http://%s:%d/%s.flv", AppConfig.PublicIP, AppConfig.Http.Port, sourceId))
	urls = append(urls, fmt.Sprintf("http://%s:%d/%s.rtc", AppConfig.PublicIP, AppConfig.Http.Port, sourceId))
	urls = append(urls, fmt.Sprintf("ws://%s:%d/%s.flv", AppConfig.PublicIP, AppConfig.Http.Port, sourceId))
	return urls
}

// DumpStream2File 保存推流到文件, 用4字节帧长分割
func DumpStream2File(sourceType SourceType, conn net.Conn, data []byte) {
	if err := os.MkdirAll("dump", 0666); err != nil {
		log.Sugar.Errorf("创建dump文件夹失败 err:%s", err.Error())
		return
	}

	path := fmt.Sprintf("dump/%s-%s", sourceType.ToString(), conn.RemoteAddr().String())
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
	GOPCache       bool   `json:"gop_cache"`       //是否开启GOP缓存，只缓存一组音视频
	GOPBufferSize  int    `json:"gop_buffer_size"` //预估GOPBuffer大小, AVPacket缓存池和合并写缓存池都会参考此大小
	ProbeTimeout   int    `json:"probe_timeout"`
	PublicIP       string `json:"public_ip"`
	ListenIP       string `json:"listen_ip"`
	IdleTimeout    int64  `json:"idle_timeout"`    //多长时间没有拉流, 单位秒. 如果开启hook通知, 根据hook响应, 决定是否关闭Source(200-不关闭/非200关闭). 否则会直接关闭Source.
	ReceiveTimeout int64  `json:"receive_timeout"` //多长时间没有收到流, 单位秒. 如果开启hook通知, 根据hook响应, 决定是否关闭Source(200-不关闭/非200关闭). 否则会直接关闭Source.
	Debug          bool   `json:"debug"`           //debug模式, 开启将保存推流

	//缓存指定时长的包，满了之后才发送给Sink. 可以降低用户态和内核态的交互频率，大幅提升性能.
	//合并写的大小范围，应当大于一帧的时长，不超过一组GOP的时长，在实际发送流的时候也会遵循此条例.
	MergeWriteLatency int `json:"mw_latency"`
	Rtmp              RtmpConfig
	Hls               HlsConfig
	JT1078            JT1078Config
	Rtsp              RtspConfig
	GB28181           GB28181Config

	Hook   HookConfig
	Record RecordConfig
	Log    LogConfig
	Http   HttpConfig
}

func LoadConfigFile(path string) (*AppConfig_, error) {
	file, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	config_ := AppConfig_{}
	if err := json.Unmarshal(file, &config_); err != nil {
		return nil, err
	}

	return &config_, err
}

func SetDefaultConfig(config_ *AppConfig_) {
	if !config_.GOPCache {
		config_.GOPCache = true
		config_.GOPBufferSize = 8196 * 1024
		config_.MergeWriteLatency = 350
		log.Sugar.Warnf("强制开启GOP缓存")
	}

	config_.GOPBufferSize = limitInt(4096*1024/8, 2048*1024*10, config_.GOPBufferSize) //最低4M码率 最高160M码率
	config_.MergeWriteLatency = limitInt(350, 2000, config_.MergeWriteLatency)         //最低缓存350毫秒数据才发送 最高缓存2秒数据才发送
	config_.ProbeTimeout = limitInt(2000, 5000, config_.MergeWriteLatency)             //2-5秒内必须解析完AVStream

	config_.Log.Level = limitInt(int(zapcore.DebugLevel), int(zapcore.FatalLevel), config_.Log.Level)
	config_.Log.MaxSize = limitMin(1, config_.Log.MaxSize)
	config_.Log.MaxBackup = limitMin(1, config_.Log.MaxBackup)
	config_.Log.MaxAge = limitMin(1, config_.Log.MaxAge)
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
