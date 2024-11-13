#!/bin/bash

# 默认的 GOOS 和 GOARCH 值
DEFAULT_GOOS="linux"
DEFAULT_GOARCH="amd64"

# 初始化 GOOS 和 GOARCH 变量
GOOS=""
GOARCH=""

# 解析命令行参数
while [ $# -gt 0 ]; do
  case "$1" in
    *=*) # 如果参数是键值对形式
      key="${1%%=*}"       # 提取键
      value="${1#*=}"      # 提取值
      case "$key" in
        GOOS) GOOS="$value" ;;  # 如果键是 GOOS，则设置 GOOS
        GOARCH) GOARCH="$value" ;;  # 如果键是 GOARCH，则设置 GOARCH
        *)      echo "Unknown parameter: $key" ;;
      esac
      shift
      ;;
    *)  # 如果参数不是键值对形式
      echo "Usage: $0 [GOOS=operating_system] [GOARCH=architecture]"
      exit 1
      ;;
  esac
done

# 如果没有通过命令行参数设置 GOOS，则尝试从 go env 读取
if [ -z "$GOOS" ]; then
  GOOS=$(go env GOOS 2>/dev/null)
  
  # 如果 go env 失败，则使用默认值
  if [ -z "$GOOS" ]; then
    GOOS="$DEFAULT_GOOS"
  fi
fi

# 如果没有通过命令行参数设置 GOARCH，则尝试从 go env 读取
if [ -z "$GOARCH" ]; then
  GOARCH=$(go env GOARCH 2>/dev/null)
  
  # 如果 go env 失败，则使用默认值
  if [ -z "$GOARCH" ]; then
    GOARCH="$DEFAULT_GOARCH"
  fi
fi

# 输出 GOOS 和 GOARCH 的值
echo "The value of GOOS is: $GOOS"
echo "The value of GOARCH is: $GOARCH"


cp ../avformat ./ -r

docker build --build-arg GOOS=$GOOS --build-arg GOARCH=$GOARCH -t lkm .
