// Package ikev2 实现 IKEv2 协议核心 (RFC 7296, RFC 5996)。
// 支持 IKE_SA_INIT, IKE_AUTH (EAP-AKA), CREATE_CHILD_SA 交换。
package ikev2

import (
	"sync"
	"time"
)

type ExchangeType uint8

const (
	ExchangeIKE_SA_INIT     ExchangeType = 34
	ExchangeIKE_AUTH        ExchangeType = 35
	ExchangeCREATE_CHILD_SA ExchangeType = 36
	ExchangeINFORMATIONAL   ExchangeType = 37
)

type NextPayload uint8

const (
	PayloadNone       NextPayload = 0
	PayloadSA         NextPayload = 33
	PayloadKE         NextPayload = 34
	PayloadIDi        NextPayload = 35
	PayloadIDr        NextPayload = 36
	PayloadCERT       NextPayload = 37
	PayloadCERTREQ    NextPayload = 38
	PayloadAUTH       NextPayload = 39
	PayloadNonce      NextPayload = 40
	PayloadNotify     NextPayload = 41
	PayloadDelete     NextPayload = 42
	PayloadVendorID   NextPayload = 43
	PayloadTSi        NextPayload = 44
	PayloadTSr        NextPayload = 45
	PayloadSK         NextPayload = 46
	PayloadCP         NextPayload = 47
	PayloadEAP        NextPayload = 48
	PayloadGSPM       NextPayload = 49
	PayloadIDrV2      NextPayload = 50
)

type ProtocolID uint8

const (
	ProtocolIKE  ProtocolID = 1
	ProtocolAH   ProtocolID = 2
	ProtocolESP  ProtocolID = 3
)

type TransformType uint8

const (
	TransformENCR       TransformType = 1
	TransformPRF        TransformType = 2
	TransformINTEG      TransformType = 3
	TransformDH         TransformType = 4
	TransformESN        TransformType = 5
)

type TransformID uint16

const (
	EncryptionENCR_3DES          TransformID = 3
	EncryptionENCR_AES_CBC_128   TransformID = 12
	EncryptionENCR_AES_CBC_192   TransformID = 13
	EncryptionENCR_AES_CBC_256   TransformID = 14
	EncryptionENCR_AES_GCM_8     TransformID = 18
	EncryptionENCR_AES_GCM_12    TransformID = 19
	EncryptionENCR_AES_GCM_16    TransformID = 20

	IntegrityAUTH_HMAC_SHA1_96   TransformID = 2
	IntegrityAUTH_HMAC_SHA2_256_128 TransformID = 12
	IntegrityAUTH_HMAC_SHA2_384_192 TransformID = 13
	IntegrityAUTH_HMAC_SHA2_512_256 TransformID = 14

	PRF_HMAC_SHA1     TransformID = 2
	PRF_HMAC_SHA2_256 TransformID = 5
	PRF_HMAC_SHA2_384 TransformID = 6
	PRF_HMAC_SHA2_512 TransformID = 7

	DHGroupNone  TransformID = 0
	DHGroup2_1024 TransformID = 2
	DHGroup5_1536 TransformID = 5
	DHGroup14_2048 TransformID = 14
	DHGroup15_3072 TransformID = 15
	DHGroup16_4096 TransformID = 16
	DHGroup17_6144 TransformID = 17
	DHGroup18_8192 TransformID = 18
)

type NotifyMessageType uint16

const (
	NotifyINITIAL_CONTACT              NotifyMessageType = 16384
	NotifySET_WINDOW_SIZE              NotifyMessageType = 16385
	NotifyNAT_DETECTION_SOURCE_IP      NotifyMessageType = 16388
	NotifyNAT_DETECTION_DESTINATION_IP NotifyMessageType = 16389
	NotifyMOBIKE_SUPPORTED             NotifyMessageType = 16396
	NotifyEAP_ONLY_AUTHENTICATION      NotifyMessageType = 16417
	NotifySIGNATURE_HASH_ALGORITHMS    NotifyMessageType = 16421
	NotifyUSE_TRANSPORT_MODE           NotifyMessageType = 16391
	NotifyESP_TFC_PADDING_NOT_SUPPORTED NotifyMessageType = 16394
	NotifyNON_FIRST_FRAGMENTS_ALSO     NotifyMessageType = 16395
)

type State int

const (
	StateInit       State = iota
	StateSAInitSent
	StateSAInitRcvd
	StateAuthSent
	StateAuthRcvd
	StateEstablished
	StateClosing
	StateClosed
	StateFailed
)

type Proposal struct {
	ProtocolID  ProtocolID
	SPI         []byte
	Transforms  []Transform
}

type Transform struct {
	Type       TransformType
	ID         TransformID
	Attributes map[uint16][]byte
}

type IKEMessage struct {
	InitiatorSPI uint64
	ResponderSPI uint64
	NextPayload  NextPayload
	Version      uint8
	ExchangeType ExchangeType
	Flags        uint8
	MessageID    uint32
	Payloads     []Payload
}

type Payload struct {
	Type NextPayload
	Data []byte
}

type SAConfig struct {
	LocalSPI  uint64
	RemoteSPI uint64
	Proposals []Proposal
}

type KeyMaterial struct {
	SK_d   []byte
	SK_ai  []byte
	SK_ar  []byte
	SK_ei  []byte
	SK_er  []byte
	SK_pi  []byte
	SK_pr  []byte
}

type IKESA struct {
	mu            sync.Mutex
	LocalSPI       uint64
	RemoteSPI      uint64
	InitiatorSPI   uint64
	ResponderSPI   uint64
	State          State
	DHGroup        TransformID
	LocalNonce     []byte
	RemoteNonce    []byte
	DHPrivate      []byte
	DHPublic       []byte
	RemoteDHPublic []byte
	SharedSecret   []byte
	Keys           *KeyMaterial
	MSK            []byte
	Proposals      []Proposal
	RemoteIdentity string
	LocalIdentity  string
	ChildSAs       map[uint32]*ChildSA
	CreatedAt      time.Time
	LastActivity   time.Time
	MessageID      uint32
}

type ChildSA struct {
	SPI          uint32
	LocalSPI     uint32
	RemoteSPI    uint32
	Protocol     ProtocolID
	TrafficSelectorsIn  []TrafficSelector
	TrafficSelectorsOut []TrafficSelector
	EncryptionKey  []byte
	IntegrityKey   []byte
}

type TrafficSelector struct {
	TSType   uint8
	IPProto  uint8
	StartPort uint16
	EndPort   uint16
	StartAddr []byte
	EndAddr   []byte
}

type Config struct {
	LocalIdentity  string
	RemoteIdentity string
	Proposals      []Proposal
	DHGroups       []TransformID
	NonceSize      int
	UseMOBIKE      bool
	UseTransport   bool
}

func DefaultConfig() *Config {
	return &Config{
		DHGroups:  []TransformID{DHGroup14_2048, DHGroup2_1024},
		NonceSize: 32,
		Proposals: []Proposal{
			{ProtocolID: ProtocolIKE, Transforms: []Transform{
				{Type: TransformENCR, ID: EncryptionENCR_AES_CBC_128},
				{Type: TransformPRF, ID: PRF_HMAC_SHA2_256},
				{Type: TransformINTEG, ID: IntegrityAUTH_HMAC_SHA2_256_128},
				{Type: TransformDH, ID: DHGroup14_2048},
			}},
			{ProtocolID: ProtocolIKE, Transforms: []Transform{
				{Type: TransformENCR, ID: EncryptionENCR_AES_CBC_128},
				{Type: TransformPRF, ID: PRF_HMAC_SHA2_256},
				{Type: TransformINTEG, ID: IntegrityAUTH_HMAC_SHA2_256_128},
				{Type: TransformDH, ID: DHGroup2_1024},
			}},
			{ProtocolID: ProtocolIKE, Transforms: []Transform{
				{Type: TransformENCR, ID: EncryptionENCR_AES_CBC_128},
				{Type: TransformPRF, ID: PRF_HMAC_SHA1},
				{Type: TransformINTEG, ID: IntegrityAUTH_HMAC_SHA1_96},
				{Type: TransformDH, ID: DHGroup2_1024},
			}},
			{ProtocolID: ProtocolIKE, Transforms: []Transform{
				{Type: TransformENCR, ID: EncryptionENCR_AES_CBC_128},
				{Type: TransformPRF, ID: PRF_HMAC_SHA1},
				{Type: TransformINTEG, ID: IntegrityAUTH_HMAC_SHA1_96},
				{Type: TransformDH, ID: DHGroup2_1024},
			}},
		},
	}
}
