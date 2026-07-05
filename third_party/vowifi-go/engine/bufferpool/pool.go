// Package bufferpool 提供零拷贝缓冲池,复用 IKEv2/IPsec 报文缓冲区。
package bufferpool

import "sync"

const (
	defaultPoolSize  = 64
	defaultBufSize   = 1500
	maxBufSize       = 65535
	jumboBufSize     = 9000
)

type Pool struct {
	pools     []sync.Pool
	bufSizes  []int
}

var defaultPool = New(defaultPoolSize, defaultBufSize)

// New 创建缓冲池。
// poolSize 控制每个大小的缓存数量上限。
// bufSize 控制默认 MTU 大小 (1500 = Ethernet)。
func New(poolSize, bufSize int) *Pool {
	p := &Pool{
		bufSizes: []int{bufSize, jumboBufSize, maxBufSize},
	}
	for _, sz := range p.bufSizes {
		s := sz
		p.pools = append(p.pools, sync.Pool{
			New: func() interface{} {
				buf := make([]byte, s)
				return &buf
			},
		})
	}
	return p
}

// Get 获取指定大小的缓冲区。优先从池中取，空时新分配。
func (p *Pool) Get(size int) []byte {
	idx := 0
	for i, sz := range p.bufSizes {
		if size <= sz {
			idx = i
			break
		}
	}
	bufPtr := p.pools[idx].Get().(*[]byte)
	return (*bufPtr)[:size]
}

// Put 归还缓冲区。
func (p *Pool) Put(buf []byte) {
	if buf == nil {
		return
	}
	cap := cap(buf)
	for i := len(p.bufSizes) - 1; i >= 0; i-- {
		if cap >= p.bufSizes[i] {
			p.pools[i].Put(&buf)
			return
		}
	}
}

// GetDefault 从默认池取。
func Get(size int) []byte {
	return defaultPool.Get(size)
}

// PutDefault 归还到默认池。
func Put(buf []byte) {
	defaultPool.Put(buf)
}
