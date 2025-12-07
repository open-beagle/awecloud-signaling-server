package logger

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/sirupsen/logrus"
)

var (
	// Log 全局日志实例
	Log *logrus.Logger
)

// InitLogrus 初始化 logrus 日志系统
func InitLogrus(level string, logFile string) error {
	Log = logrus.New()

	// 设置日志级别
	logLevel, err := parseLogLevel(level)
	if err != nil {
		logLevel = logrus.InfoLevel
		Log.Warnf("无效的日志级别 '%s'，使用默认级别 'info'", level)
	}
	Log.SetLevel(logLevel)

	// 设置日志格式
	Log.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02 15:04:05",
		DisableColors:   false,
		ForceColors:     false,
	})

	// 设置日志输出
	if logFile != "" {
		// 创建日志目录
		if err := os.MkdirAll("logs", 0755); err != nil {
			return err
		}

		// 创建轮转日志Writer
		rotatingWriter, err := NewRotatingWriter(logFile)
		if err != nil {
			return err
		}

		// 同时输出到文件和标准输出
		multiWriter := io.MultiWriter(os.Stdout, rotatingWriter)
		Log.SetOutput(multiWriter)
	} else {
		// 只输出到标准输出
		Log.SetOutput(os.Stdout)
	}

	Log.Infof("日志系统初始化完成 - 级别: %s, 文件: %s", level, logFile)
	return nil
}

// parseLogLevel 解析日志级别字符串
func parseLogLevel(level string) (logrus.Level, error) {
	switch strings.ToLower(level) {
	case "trace":
		return logrus.TraceLevel, nil
	case "debug":
		return logrus.DebugLevel, nil
	case "info":
		return logrus.InfoLevel, nil
	case "warn", "warning":
		return logrus.WarnLevel, nil
	case "error":
		return logrus.ErrorLevel, nil
	case "fatal":
		return logrus.FatalLevel, nil
	case "panic":
		return logrus.PanicLevel, nil
	default:
		return logrus.InfoLevel, fmt.Errorf("unknown log level: %s", level)
	}
}

// GetLogLevel 获取当前日志级别字符串
func GetLogLevel() string {
	if Log == nil {
		return "info"
	}
	return Log.GetLevel().String()
}

// 便捷方法 - 兼容标准库 log 包的接口

// Printf 格式化输出 Info 级别日志（兼容标准库）
func Printf(format string, args ...interface{}) {
	if Log != nil {
		Log.Infof(format, args...)
	}
}

// Println 输出 Info 级别日志（兼容标准库）
func Println(args ...interface{}) {
	if Log != nil {
		Log.Infoln(args...)
	}
}

// Print 输出 Info 级别日志（兼容标准库）
func Print(args ...interface{}) {
	if Log != nil {
		Log.Info(args...)
	}
}

// Fatalf 格式化输出 Fatal 级别日志并退出（兼容标准库）
func Fatalf(format string, args ...interface{}) {
	if Log != nil {
		Log.Fatalf(format, args...)
	} else {
		logrus.Fatalf(format, args...)
	}
}

// 便捷方法 - logrus 风格

// Debug 输出 Debug 级别日志
func Debug(args ...interface{}) {
	if Log != nil {
		Log.Debug(args...)
	}
}

// Debugf 格式化输出 Debug 级别日志
func Debugf(format string, args ...interface{}) {
	if Log != nil {
		Log.Debugf(format, args...)
	}
}

// Info 输出 Info 级别日志
func Info(args ...interface{}) {
	if Log != nil {
		Log.Info(args...)
	}
}

// Infof 格式化输出 Info 级别日志
func Infof(format string, args ...interface{}) {
	if Log != nil {
		Log.Infof(format, args...)
	}
}

// Warn 输出 Warn 级别日志
func Warn(args ...interface{}) {
	if Log != nil {
		Log.Warn(args...)
	}
}

// Warnf 格式化输出 Warn 级别日志
func Warnf(format string, args ...interface{}) {
	if Log != nil {
		Log.Warnf(format, args...)
	}
}

// Error 输出 Error 级别日志
func Error(args ...interface{}) {
	if Log != nil {
		Log.Error(args...)
	}
}

// Errorf 格式化输出 Error 级别日志
func Errorf(format string, args ...interface{}) {
	if Log != nil {
		Log.Errorf(format, args...)
	}
}

// Fatal 输出 Fatal 级别日志并退出
func Fatal(args ...interface{}) {
	if Log != nil {
		Log.Fatal(args...)
	} else {
		logrus.Fatal(args...)
	}
}

// WithField 创建带字段的日志条目
func WithField(key string, value interface{}) *logrus.Entry {
	if Log != nil {
		return Log.WithField(key, value)
	}
	return logrus.NewEntry(logrus.StandardLogger()).WithField(key, value)
}

// WithFields 创建带多个字段的日志条目
func WithFields(fields logrus.Fields) *logrus.Entry {
	if Log != nil {
		return Log.WithFields(fields)
	}
	return logrus.NewEntry(logrus.StandardLogger()).WithFields(fields)
}
