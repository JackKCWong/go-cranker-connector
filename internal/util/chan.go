package util

type Flare struct {
	ch chan struct{}
}

func NewFlare() *Flare {
	return &Flare{
		ch: make(chan struct{}),
	}
}

func (f *Flare) Fire() {
	close(f.ch)
}

func (f *Flare) Flashed() <-chan struct{} {
	return f.ch
}


type SigChan chan struct{}
