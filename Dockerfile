FROM golang:1.19-alpine as builder

# 设置构建参数
ARG GOOS=linux
ARG GOARCH=amd64

# 输出参数值以便调试
RUN echo "Building for GOOS: $GOOS and GOARCH: $GOARCH"

 
# 设置必要的环境变量
ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    GOOS=$GOOS \
    GOARCH=$GOARCH \
    GOPROXY=https://goproxy.cn,direct
 
RUN set -ex \
    && sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories \
    && apk --update add tzdata \
    && cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime \
    && apk --no-cache add ca-certificates
 
# 移动到工作目录：/build
WORKDIR /build/lkm
 
# 将代码复制到容器中
COPY . .

COPY ./avformat /build/avformat
 
RUN go mod download && go mod tidy -v && go build -o lkm .
 
# 运行阶段指定scratch作为基础镜像
FROM scratch
 
WORKDIR /app
 
# 拷贝二进制可执行文件
COPY --from=builder /build/lkm/lkm lkm

COPY --from=builder /build/lkm/config.json config.json
 
# 下载时区包
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
 
# 设置当前时区
COPY --from=builder /usr/share/zoneinfo/Asia/Shanghai /etc/localtime
  
# 需要运行的命令
ENTRYPOINT ["./lkm"]

