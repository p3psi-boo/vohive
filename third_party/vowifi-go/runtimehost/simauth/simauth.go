// Package simauth 提供 SIM 认证接口。
package simauth

// Provider SIM 认证提供者。
type Provider interface {
	GetAKA(rand, autn []byte) (res []byte, ck []byte, ik []byte, err error)
	GetIMSI() string
}
