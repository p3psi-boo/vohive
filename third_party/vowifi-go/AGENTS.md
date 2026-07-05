# AGENTS.md — vowifi-go

## 项目概述

**GitHub**: sunnyhmz7010/vowifi-go
**Go 模块路径**: github.com/iniwex5/vowifi-go
**Go 版本**: 1.26.0

VoWiFi 协议引擎，提供 IKEv2/IPsec ESP/SIP/IMS/EAP-AKA 完整协议栈，由 vohive 通过 runtimehost 接口调用。

## 参照来源

- **SimAdmin Rust** (31 个 `.rs` 文件): nkguo-simadmin/backend/src/vowifi/
  - ike.rs + ike_state.rs (124K) / ike_payloads.rs (37K) / ike_codec.rs
  - ims.rs (39K) / sms.rs (58K) / dataplane.rs (60K) / live.rs (224K)
  - tun_gateway.rs (33K) / transport.rs + epdg.rs / profiles.rs (28K)
- **RFC 标准**: RFC 7296 (IKEv2) / RFC 4303 (ESP) / RFC 4187 (EAP-AKA) / 3GPP TS 24.229 (IMS)
- **接口契约**: 由 vohive 编译驱动 + ELF 符号表反推确定

## 目录结构

```
engine/
  crypto/         DH MODP (2/14) / AES-CBC / HMAC-SHA256 / AES-CMAC / PRF+
  eap/            EAP-AKA MILENAGE (f1-f5) / CK'/IK' 派生 / MSK
  ikev2/          IKEv2 状态机 (SA_INIT/AUTH/CREATE_CHILD_SA)
  ipsec/          ESP 加密/解密 (AES-CBC-256 + HMAC-SHA2-256)
  driver/         TUN 设备 ioctl (Linux-only)
  bufferpool/     零拷贝缓冲池
  logger/         结构化日志接口
  sim/swu/        类型定义

internal/vowifi/
  runtimecore/    运行时协调器 (IKE→IPsec→IMS)
  imscore/        IMS 注册引擎 + SMS over IP
  sipkit/         SIP 协议栈 (REGISTER/MESSAGE + Digest Auth)
  dns/epdg/       ePDG FQDN 解析与选择
  smscodec/       SMS TPDU 编解码
  profile/policy/ 运营商配置与策略
  startup/        启动序列编排
  voice/ipsec3gpp/entitlement/ 语音/3GPP IPsec/E911

runtimehost/
  接口层 (vohive 直接调用的公共 API)
  - facade.go: State/Modem/SIMAdapter/StartRequest/ProxyConfig/DataplanePolicy
  - carrier/identity/messaging/eventhost/voicehost/e911/simauth/voiceclient
```

## 编译

```bash
# 完整编译 (Linux)
GOOS=linux go build ./...

# 单独编译引擎层 (跨平台)
go build ./engine/crypto ./engine/eap ./engine/ikev2 ./engine/ipsec ./engine/logger
```

内部包 `engine/driver` 使用 `//go:build linux` 约束, 仅 Linux 平台编译。

## 接口约束

| vohive import | 说明 |
|---|---|
| `runtimehost` | 核心类型 (State/Phase/Modem/SIMAdapter/StartRequest) |
| `runtimehost/carrier` | 运营商配置 |
| `runtimehost/identity` | 身份管理 (IMPI/IMPU/ISIM) |
| `runtimehost/messaging` | SMS/USSD 消息接口 |
| `runtimehost/eventhost` | 事件分发 |
| `runtimehost/voicehost` | 语音网关 |
| `runtimehost/e911` | E911 紧急呼叫 |
| `engine/sim` | SIM AKA 类型 |
| `engine/swu` | 数据面模式 |

这些包的类型签名必须与 vohive 编译对齐, vohive `go build -mod=vendor` 通过即表示兼容。

## 关键决策

- 加密原语全部使用 Go 标准库 `crypto/*`, 不引入第三方加密库
- `engine/driver` 仅 Linux, 其他包跨平台编译
- `runtimehost/` 接口签名由 vohive 编译驱动确定, 修改需同步验证 vohive 编译
- 运营商配置主要参照 SimAdmin profiles.rs (AT&T/T-Mobile 内置)

## 操作检查清单

- [ ] 新函数签名后检查 import 是否完整
- [ ] 不在 crypto.go 和 dh.go 中重复声明 DHGenerate/DHCompute
- [ ] 执行 GOOS=linux go build ./... 验证编译
- [ ] SimAdmin 参照目录: C:\Users\Sunny\下载\SimAdmin-main（含vowifi代码）\
- [ ] IKE/ESP 加密: 不用 PKCS7, 用 counter padding (0x00,0x01,...)
- [ ] 密钥派生: SKEYSEED 先于 PRF+ (RFC 7296 §2.14)
- [ ] AUTH payload: 含 SA_init + Nr + IDi_hash (RFC 7296 §2.16)
- [ ] NAI: MNC 零填充 (%03s), 不直接用原始值
- [ ] DH: 输出定宽 (leftPadToLen), 不用 big.Int.Bytes() 原生值
- [ ] 修改后编译: GOOS=linux go build ./... && cd ../vohive && GOOS=linux GOARCH=arm64 go build -mod=vendor ./...
