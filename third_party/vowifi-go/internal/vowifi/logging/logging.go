// Package logging 提供 VoWiFi IMS 协议日志记录。
package logging

import (
	"os"
	"sync"
)

type Logger struct {
	mu   sync.Mutex
	file *os.File
}

func New(path string) (*Logger, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	return &Logger{file: f}, nil
}

func (l *Logger) Write(data []byte) (int, error) {
	if l == nil || l.file == nil {
		return 0, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.file.Write(append(data, '\n'))
}

func (l *Logger) LogProtocol(protocol, direction, message string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.file.WriteString(protocol + " " + direction + " " + message + "\n")
}

func (l *Logger) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	return l.file.Close()
}
