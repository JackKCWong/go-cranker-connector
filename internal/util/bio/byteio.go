package bio

import "io"

func Dump(buf []byte, src io.Reader) (int, error) {
	cur := 0
	for {
		n, err := src.Read(buf[cur:])
		cur += n
		if err != nil {
			return cur, err
		}
	}
}
