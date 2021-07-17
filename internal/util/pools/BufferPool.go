package pools

import (
	"bytes"
	"sync"
)

type BufferPool struct {
	pool *sync.Pool
}

func NewBufferPool() *BufferPool {
	return &BufferPool{
		pool: &sync.Pool{New: func() interface{} {
			return &bytes.Buffer{}
		}},
	}
}


func (p *BufferPool) Get() *bytes.Buffer {
	return p.pool.Get().(*bytes.Buffer)
}


func (p *BufferPool) Release(b *bytes.Buffer) {
	b.Reset()
	p.pool.Put(b)
}