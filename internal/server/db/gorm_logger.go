package db

import (
	"context"
	"fmt"
	"time"

	gormlogger "gorm.io/gorm/logger"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
)

// GormLogger 自定义 GORM 日志适配器
type GormLogger struct {
	LogLevel gormlogger.LogLevel
}

// NewGormLogger 创建自定义 GORM logger
func NewGormLogger(level gormlogger.LogLevel) *GormLogger {
	return &GormLogger{
		LogLevel: level,
	}
}

// LogMode 设置日志级别
func (l *GormLogger) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	newLogger := *l
	newLogger.LogLevel = level
	return &newLogger
}

// Info 输出 Info 级别日志
func (l *GormLogger) Info(ctx context.Context, msg string, data ...interface{}) {
	if l.LogLevel >= gormlogger.Info {
		logger.Infof("[gorm] "+msg, data...)
	}
}

// Warn 输出 Warn 级别日志
func (l *GormLogger) Warn(ctx context.Context, msg string, data ...interface{}) {
	if l.LogLevel >= gormlogger.Warn {
		logger.Warnf("[gorm] "+msg, data...)
	}
}

// Error 输出 Error 级别日志
func (l *GormLogger) Error(ctx context.Context, msg string, data ...interface{}) {
	if l.LogLevel >= gormlogger.Error {
		logger.Errorf("[gorm] "+msg, data...)
	}
}

// Trace 输出 SQL 执行日志
func (l *GormLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	if l.LogLevel <= gormlogger.Silent {
		return
	}

	elapsed := time.Since(begin)
	sql, rows := fc()

	switch {
	case err != nil && l.LogLevel >= gormlogger.Error:
		logger.Errorf("[gorm] %s [%.3fms] [rows:%d] %s", err, float64(elapsed.Nanoseconds())/1e6, rows, sql)
	case elapsed > 200*time.Millisecond && l.LogLevel >= gormlogger.Warn:
		logger.Warnf("[gorm] SLOW SQL >= 200ms [%.3fms] [rows:%d] %s", float64(elapsed.Nanoseconds())/1e6, rows, sql)
	case l.LogLevel >= gormlogger.Info:
		logger.Infof("[gorm] [%.3fms] [rows:%d] %s", float64(elapsed.Nanoseconds())/1e6, rows, sql)
	}
}

// ParamsFilter 参数过滤器（用于隐藏敏感信息）
func (l *GormLogger) ParamsFilter(ctx context.Context, sql string, params ...interface{}) (string, []interface{}) {
	// 可以在这里过滤敏感参数
	return sql, params
}

// formatSQL 格式化 SQL（简化版）
func formatSQL(sql string) string {
	// 移除多余的空格和换行
	return fmt.Sprintf("%s", sql)
}
