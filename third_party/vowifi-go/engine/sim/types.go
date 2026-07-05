// Package sim 提供 SWU (Software Update) 引擎使用的 SIM/UICC 鉴权类型。
// vohive 通过此包定义 AKAProvider 接口，内部用 AT/QMI 实现 SIM 鉴权。
package sim

import "errors"

// ErrSyncFailure 表示 AKA 同步失败，需通过 AUTS 重新同步。
var ErrSyncFailure = errors.New("aka sync failure")

// AKAResult 保存 AKA (Authentication and Key Agreement) 计算结果。
type AKAResult struct {
	RES  []byte
	CK   []byte
	IK   []byte
	AUTS []byte
}

// AKAProvider 定义 AKA 计算接口。
// vohive 通过 internal/sim 下的 ATAKAProvider（AT 指令）或 backendAKAProvider（MBIM Auth）实现。
type AKAProvider interface {
	CalculateAKA(rand16, autn16 []byte) (AKAResult, error)
}
