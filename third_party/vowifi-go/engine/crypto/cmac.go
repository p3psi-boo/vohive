package crypto

import (
	"crypto/cipher"
	"fmt"
)

func cmacCompute(block cipher.Block, data []byte) []byte {
	k0 := make([]byte, block.BlockSize())
	block.Encrypt(k0, k0)

	k1 := cmacSubkey(k0)
	k1 = cmacSubkey(k1)

	blockSize := block.BlockSize()

	padded := make([]byte, ((len(data)+blockSize-1)/blockSize)*blockSize)
	copy(padded, data)
	complete := len(data)%blockSize == 0 && len(data) > 0

	if complete {
		for i := 0; i < blockSize; i++ {
			padded[len(padded)-blockSize+i] ^= k1[i]
		}
	} else {
		padded[len(data)] = 0x80
		for i := 0; i < blockSize; i++ {
			padded[len(padded)-blockSize+i] ^= k0[i]
		}
	}

	x := make([]byte, blockSize)
	for i := 0; i < len(padded); i += blockSize {
		for j := 0; j < blockSize; j++ {
			x[j] ^= padded[i+j]
		}
		block.Encrypt(x, x)
	}
	return x
}

func cmacSubkey(k []byte) []byte {
	msb := k[0] & 0x80
	k2 := make([]byte, len(k))
	copy(k2, k)
	shiftLeft(k2)
	if msb != 0 {
		switch len(k2) {
		case 16:
			k2[len(k2)-1] ^= 0x87
		case 32:
			k2[len(k2)-1] ^= 0x1b
		default:
			panic(fmt.Sprintf("cmac: unsupported block size %d", len(k2)))
		}
	}
	return k2
}

func shiftLeft(b []byte) {
	carry := byte(0)
	for i := len(b) - 1; i >= 0; i-- {
		next := b[i] >> 7
		b[i] = (b[i] << 1) | carry
		carry = next
	}
}
