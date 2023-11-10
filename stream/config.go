package stream

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

type AppConfig_ struct {
	Rtmp RtmpConfig
	Hook HookConfig
}
