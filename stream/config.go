package stream

const (
	DefaultMergeWriteLatency = 350
)

type RtmpConfig struct {
	Enable bool   `json:"enable"`
	Addr   string `json:"addr"`
}

type HookConfig struct {
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
	GOPCache          bool `json:"gop_cache"` //是否开启GOP缓存，只缓存一组音视频
	ProbeTimeout      int  `json:"probe_timeout"`
	MergeWriteLatency int  `json:"mw_latency"` //缓存指定时长的包，满了之后才发送给Sink. 可以降低用户态和内核态的交互，大幅提升性能.
	Rtmp              RtmpConfig
	Hook              HookConfig
}
