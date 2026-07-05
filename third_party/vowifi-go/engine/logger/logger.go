// Package logger 提供 vowifi-go 引擎内部结构化日志接口。
// 默认接入宿主 (vohive) 的 zap logger,也可独立使用标准库 log。
package logger

import (
	"log"
	"sync"
)

type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

type Logger interface {
	Debug(msg string, keysAndValues ...interface{})
	Info(msg string, keysAndValues ...interface{})
	Warn(msg string, keysAndValues ...interface{})
	Error(msg string, keysAndValues ...interface{})
}

var (
	global Logger
	mu     sync.RWMutex
)

func init() {
	global = &stdLogger{level: LevelInfo}
}

// SetLogger 设置全局日志器。
func SetLogger(l Logger) {
	if l == nil {
		return
	}
	mu.Lock()
	global = l
	mu.Unlock()
}

// GetLogger 获取全局日志器。
func GetLogger() Logger {
	mu.RLock()
	defer mu.RUnlock()
	return global
}

func Debug(msg string, keysAndValues ...interface{}) {
	GetLogger().Debug(msg, keysAndValues...)
}

func Info(msg string, keysAndValues ...interface{}) {
	GetLogger().Info(msg, keysAndValues...)
}

func Warn(msg string, keysAndValues ...interface{}) {
	GetLogger().Warn(msg, keysAndValues...)
}

func Error(msg string, keysAndValues ...interface{}) {
	GetLogger().Error(msg, keysAndValues...)
}

// stdLogger 标准库 log 适配器。
type stdLogger struct {
	level Level
	mu    sync.Mutex
}

func (l *stdLogger) Debug(msg string, keysAndValues ...interface{}) {
	if l.level <= LevelDebug {
		l.mu.Lock()
		log.Printf("[DEBUG] "+msg, keysAndValues...)
		l.mu.Unlock()
	}
}

func (l *stdLogger) Info(msg string, keysAndValues ...interface{}) {
	if l.level <= LevelInfo {
		l.mu.Lock()
		log.Printf("[INFO] "+msg, keysAndValues...)
		l.mu.Unlock()
	}
}

func (l *stdLogger) Warn(msg string, keysAndValues ...interface{}) {
	if l.level <= LevelWarn {
		l.mu.Lock()
		log.Printf("[WARN] "+msg, keysAndValues...)
		l.mu.Unlock()
	}
}

func (l *stdLogger) Error(msg string, keysAndValues ...interface{}) {
	if l.level <= LevelError {
		l.mu.Lock()
		log.Printf("[ERROR] "+msg, keysAndValues...)
		l.mu.Unlock()
	}
}
