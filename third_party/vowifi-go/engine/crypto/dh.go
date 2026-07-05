package crypto

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

func dhGroup2Prime() *big.Int {
	p, _ := new(big.Int).SetString("FFFFFFFFFFFFFFFFC90FDAA22168C234C4C6628B80DC1CD129024E088A67CC74020BBEA63B139B22514A08798E3404DDEF9519B3CD3A431B302B0A6DF25F14374FE1356D6D51C245E485B576625E7EC6F44C42E9A637ED6B0BFF5CB6F406B7EDEE386BFB5A899FA5AE9F24117C4B1FE649286651ECE65381FFFFFFFFFFFFFFFF", 16)
	return p
}

func dhGroup2Generator() *big.Int { return big.NewInt(2) }

func dhGroup5Prime() *big.Int {
	p, _ := new(big.Int).SetString("FFFFFFFFFFFFFFFFC90FDAA22168C234C4C6628B80DC1CD129024E088A67CC74020BBEA63B139B22514A08798E3404DDEF9519B3CD3A431B302B0A6DF25F14374FE1356D6D51C245E485B576625E7EC6F44C42E9A637ED6B0BFF5CB6F406B7EDEE386BFB5A899FA5AE9F24117C4B1FE649286651ECE45B3DC2007CB8A163BF0598DA48361C55D39A69163FA8FD24CF5F83655D23DCA3AD961C62F356208552BB9ED529077096966D670C354E4ABC9804F1746C08CA237327FFFFFFFFFFFFFFFF", 16)
	return p
}

func dhGroup5Generator() *big.Int { return big.NewInt(2) }

func dhGroup14Prime() *big.Int {
	p, _ := new(big.Int).SetString("FFFFFFFFFFFFFFFFC90FDAA22168C234C4C6628B80DC1CD129024E088A67CC74020BBEA63B139B22514A08798E3404DDEF9519B3CD3A431B302B0A6DF25F14374FE1356D6D51C245E485B576625E7EC6F44C42E9A637ED6B0BFF5CB6F406B7EDEE386BFB5A899FA5AE9F24117C4B1FE649286651ECE45B3DC2007CB8A163BF0598DA48361C55D39A69163FA8FD24CF5F83655D23DCA3AD961C62F356208552BB9ED529077096966D670C354E4ABC9804F1746C08CA18217C32905E462E36CE3BE39E772C180E86039B2783A2EC07A28FB5C55DF06F4C52C9DE2BCBF6955817183995497CEA956AE515D2261898FA051015728E5A8AACAA68FFFFFFFFFFFFFFFF", 16)
	return p
}

func dhGroup14Generator() *big.Int { return big.NewInt(2) }

// DHGenerate 生成指定 DH 组的密钥对，公钥/私钥均固定宽度。
func DHGenerate(group int) (*DHKeyPair, error) {
	byteLen := dhByteLen(group)
	switch group {
	case DHGroup2:
		return dhModp(group, dhGroup2Prime(), dhGroup2Generator(), byteLen)
	case DHGroup5:
		return dhModp(group, dhGroup5Prime(), dhGroup5Generator(), byteLen)
	case DHGroup14:
		return dhModp(group, dhGroup14Prime(), dhGroup14Generator(), byteLen)
	default:
		return nil, fmt.Errorf("unsupported DH group: %d", group)
	}
}

// DHCompute 计算共享密钥，返回定长字节。
func DHCompute(group int, privateKey, peerPublic []byte) ([]byte, error) {
	byteLen := dhByteLen(group)
	switch group {
	case DHGroup2:
		return dhComputeModp(dhGroup2Prime(), privateKey, peerPublic, byteLen)
	case DHGroup5:
		return dhComputeModp(dhGroup5Prime(), privateKey, peerPublic, byteLen)
	case DHGroup14:
		return dhComputeModp(dhGroup14Prime(), privateKey, peerPublic, byteLen)
	default:
		return nil, fmt.Errorf("unsupported DH group: %d", group)
	}
}

func dhByteLen(group int) int {
	switch group {
	case DHGroup2:
		return 128
	case DHGroup5:
		return 192
	case DHGroup14:
		return 256
	default:
		return 256
	}
}

func dhModp(group int, prime, generator *big.Int, byteLen int) (*DHKeyPair, error) {
	for {
		priv, err := rand.Int(rand.Reader, prime)
		if err != nil {
			return nil, fmt.Errorf("rand.Int: %w", err)
		}
		if priv.Cmp(big.NewInt(1)) <= 0 {
			continue
		}
		pub := new(big.Int).Exp(generator, priv, prime)
		return &DHKeyPair{
			Group:      group,
			PrivateKey: leftPadToLen(priv.Bytes(), byteLen),
			PublicKey:  leftPadToLen(pub.Bytes(), byteLen),
		}, nil
	}
}

func dhComputeModp(prime *big.Int, privateBytes, peerPublicBytes []byte, byteLen int) ([]byte, error) {
	priv := new(big.Int).SetBytes(privateBytes)
	pub := new(big.Int).SetBytes(peerPublicBytes)
	two := big.NewInt(2)
	if pub.Cmp(two) < 0 || pub.Cmp(new(big.Int).Sub(prime, two)) > 0 {
		return nil, fmt.Errorf("peer public key out of range [2, p-2]")
	}
	shared := new(big.Int).Exp(pub, priv, prime)
	return leftPadToLen(shared.Bytes(), byteLen), nil
}

func leftPadToLen(b []byte, targetLen int) []byte {
	if len(b) >= targetLen {
		return b
	}
	padded := make([]byte, targetLen)
	copy(padded[targetLen-len(b):], b)
	return padded
}
