// Package common 提供 VoWiFi 共享工具函数。
package common

import "fmt"

// HexDump 返回字节数组的十六进制表示。
func HexDump(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	return fmt.Sprintf("%x", data)
}

// Truncate 截断字符串到指定长度。
func Truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// NonNilString 返回 s 或 default_。
func NonNilString(s *string, default_ string) string {
	if s == nil {
		return default_
	}
	return *s
}
