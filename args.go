package main

import (
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/stream"
	"os"
	"strconv"
	"strings"
)

func readRunArgs() (map[string]string, map[string]string) {
	args := os.Args

	// 运行参数项优先级高于config.json参数项
	// --disable-rtmp 		--enable-rtmp=11935
	// --disable-rtsp 		--enable-rtsp
	// --disable-hls  		--enable-hls
	// --disable-webrtc 	--enable-webrtc=18000
	// --disable-gb28181 	--enable-gb28181
	// --disable-jt1078		--enable-jt1078=11078
	// --disable-hooks		--enable-hooks
	// --disable-record 	--enable-record

	disableOptions := map[string]string{}
	enableOptions := map[string]string{}
	for _, arg := range args {
		// 参数忽略大小写
		arg = strings.ToLower(arg)

		var option string
		var enable bool
		if strings.HasPrefix(arg, "--disable-") {
			option = arg[len("--disable-"):]
		} else if strings.HasPrefix(arg, "--enable-") {
			option = arg[len("--enable-"):]
			enable = true
		} else {
			continue
		}

		pair := strings.Split(option, "=")
		var value string
		if len(pair) > 1 {
			value = pair[1]
		}

		if enable {
			enableOptions[pair[0]] = value
		} else {
			disableOptions[pair[0]] = value
		}
	}

	// 删除重叠参数, 禁用和开启同时声明时, 以开启为准.
	for k := range enableOptions {
		if _, ok := disableOptions[k]; ok {
			delete(disableOptions, k)
		}
	}

	return disableOptions, enableOptions
}

func mergeArgs(options map[string]stream.EnableConfig, disableOptions, enableOptions map[string]string) {
	for k := range disableOptions {
		option, ok := options[k]
		utils.Assert(ok)

		option.SetEnable(false)
	}

	for k, v := range enableOptions {
		var port int

		if len(v) > 0 {
			atoi, err := strconv.Atoi(v)
			if err == nil && atoi > 0 {
				port = atoi
			}
		}

		option, ok := options[k]
		utils.Assert(ok)

		option.SetEnable(true)

		if port > 0 {
			if config, ok := option.(stream.PortConfig); ok {
				config.SetPort(port)
			}
		}
	}
}
