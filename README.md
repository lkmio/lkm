## 简介

基于GoLang实现的流媒体服务器，支持RTMP、GB28181、1078推流，输出rtmp/http-flv/ws-flv/webrtc/hls/rtsp等拉流协议。支持如下编码器和流协议：

| Codec\Stream | RTMP | FLV | HLS | RTC | RTSP |
| ------------ | ---- | --- | --- | --- | ---- |
| H264         | √    | √   | √   | √   | √    |
| H265         | √    | √   | √   | -([有计划支持](https://linkingvision.com/webrtch265))   | √    |
| G711A/U      | √    | √   | -   | √   | √    |
| AAC          | √    | √   | √   | -   | √    |
| OPUS         | -    | -   | -   | √   | -    |

## 编译

在使用之前，建议先阅读[LKM启动配置文件参数说明](https://github.com/lkmio/lkm/wiki/Startup-Parameters)。如果你想修改源码，推荐阅读[LKM源码分析](https://github.com/lkmio/lkm/wiki/Source-Code-Analysis)。

### 源码编译

     git clone https://github.com/lkmio/avformat.git
     git clone https://github.com/lkmio/lkm.git
     cd lkm
     go mod tidy
     go mod vendor
     go build

### docker编译

     ./build_docker_images.sh GOOS=linux GOARCH=amd64


支持修改`GOOS`和`GOARCH`参数来决定编译平台。默认编译制作`linx amd64`平台的镜像，如果宿主机有golang编译环境，则以宿主机平台为准。优先级如下：编译时指定平台 > 宿主机平台 > 默认平台。

### docker启动

* 目前还未发布到dockerhub

```
sudo docker run --log-driver json-file --log-opt max-size=10m --network=host -it lkm:latest /bin/sh
```



## RTMP推流

ffmpeg推流示例：

    ffmpeg -re -i ./232937384-1-208_baseline.mp4 -c copy -f flv rtmp://127.0.0.1/hls/mystream

拉流地址示例：

    [
    	"rtmp://192.168.2.148:1935/hls/mystream",
    	"rtsp://192.168.2.148:554/hls/mystream",
    	"http://192.168.2.148:8080/hls/mystream.flv",
    	"http://192.168.2.148:8080/hls/mystream.rtc",
    	"ws://192.168.2.148:8080/hls/mystream.flv"
    ]

## GB28181推流

1.  [安装信令服务器](https://github.com/lkmio/gb-cms)
2.  配置[http hooks](https://github.com/lkmio/lkm/wiki/Startup-Parameters#hook)
3.  查询在线设备
> curl -v http://localhost:9000/api/v1/device/list
3.  使用ffplay播放

```
// 实时预览-UDP方式 34020000001320000001设备下的34020000001310000001通道
ffplay -i rtmp://127.0.0.1/34020000001320000001/34020000001310000001
// 实时预览-TCP被动方式 34020000001320000001设备下的34020000001310000001通道
ffplay -i rtmp://127.0.0.1/34020000001320000001/34020000001310000001?setup=passive
ffplay -i http://127.0.0.1:8080/34020000001320000001/34020000001310000001.flv?setup=passive
ffplay -i http://127.0.0.1:8080/34020000001320000001/34020000001310000001.m3u8?setup=passive
ffplay -i rtsp://test:123456@127.0.0.1/34020000001320000001/34020000001310000001?setup=passive
// 回放-TCP被动方式 34020000001320000001设备下的34020000001310000001通道
ffplay -i rtmp://127.0.0.1/34020000001320000001/34020000001310000001.session_id_0?setup=passive&stream_type=playback&start_time=2024-06-18T15:20:56&end_time=2024-06-18T15:25:56

```

## 1078推流

> 需自行安装信令服务, 告知设备推流到LKM的收流端口

