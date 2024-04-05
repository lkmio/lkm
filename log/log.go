package log

import (
	"fmt"
	"github.com/natefinch/lumberjack"
	"github.com/yangjiechina/avformat/utils"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"os"
	"strings"
)

var (
	Sugar *zap.SugaredLogger
)

func InitLogger(leve zapcore.LevelEnabler,
	name string, maxSize, maxBackup, maxAge int, compress bool) {
	utils.Assert(Sugar == nil)

	var sinks []zapcore.Core
	writeSyncer := getLogWriter(name, maxSize, maxBackup, maxAge, compress)
	encoder := getEncoder()

	fileCore := zapcore.NewCore(encoder, writeSyncer, leve)

	sinks = append(sinks, fileCore)
	//打印到控制台
	sinks = append(sinks, zapcore.NewCore(encoder, zapcore.AddSync(os.Stdout), leve))

	core := zapcore.NewTee(sinks...)

	logger := zap.New(core, zap.AddCaller())
	Sugar = logger.Sugar()
}

func getEncoder() zapcore.Encoder {
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
	return zapcore.NewConsoleEncoder(encoderConfig)
}

// 配置日志保存规则
// @name      日志文件名, 可包含路径
// @maxSize   单个日志文件最大大小(M)
// @maxBackup 日志文件最多生成多少个
// @maxAge	  日志文件最多保存多少天
func getLogWriter(name string, maxSize, maxBackup, maxAge int, compress bool) zapcore.WriteSyncer {
	lumberJackLogger := &lumberjack.Logger{
		Filename:   name,
		MaxSize:    maxSize,
		MaxBackups: maxBackup,
		MaxAge:     maxAge,
		Compress:   compress,
	}
	return zapcore.AddSync(lumberJackLogger)
}

// Logger interface used as base logger throughout the library.
type Logger interface {
	Print(args ...interface{})
	Printf(format string, args ...interface{})

	Trace(args ...interface{})
	Tracef(format string, args ...interface{})

	Debug(args ...interface{})
	Debugf(format string, args ...interface{})

	Info(args ...interface{})
	Infof(format string, args ...interface{})

	Warn(args ...interface{})
	Warnf(format string, args ...interface{})

	Error(args ...interface{})
	Errorf(format string, args ...interface{})

	Fatal(args ...interface{})
	Fatalf(format string, args ...interface{})

	Panic(args ...interface{})
	Panicf(format string, args ...interface{})

	WithPrefix(prefix string) Logger
	Prefix() string

	WithFields(fields Fields) Logger
	Fields() Fields

	SetLevel(level Level)
}

type Loggable interface {
	Log() Logger
}

type Fields map[string]interface{}

func (fields Fields) String() string {
	str := make([]string, 0)

	for k, v := range fields {
		str = append(str, fmt.Sprintf("%s=%+v", k, v))
	}

	return strings.Join(str, " ")
}

func (fields Fields) WithFields(newFields Fields) Fields {
	allFields := make(Fields)

	for k, v := range fields {
		allFields[k] = v
	}

	for k, v := range newFields {
		allFields[k] = v
	}

	return allFields
}

func AddFieldsFrom(logger Logger, values ...interface{}) Logger {
	for _, value := range values {
		switch v := value.(type) {
		case Logger:
			logger = logger.WithFields(v.Fields())
		case Loggable:
			logger = logger.WithFields(v.Log().Fields())
		case interface{ Fields() Fields }:
			logger = logger.WithFields(v.Fields())
		}
	}
	return logger
}
