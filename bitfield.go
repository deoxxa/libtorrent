package libtorrent

import (
	"math"
)

type Bitfield struct {
	data   []byte
	length int
}

func NewBitfield(data []byte, length int) *Bitfield {
	if data == nil {
		data = make([]byte, int(math.Ceil(float64(length)/8)))
	}

	return &Bitfield{
		data:   data,
		length: length,
	}
}

func (bf *Bitfield) Bytes() []byte {
	return bf.data
}

func (bf *Bitfield) Length() int {
	return bf.length
}

func (bf *Bitfield) Set(i int, v bool) {
	b := uint(i-(i%8)) / 8
	c := uint(7 - (i % 8))

	if v {
		bf.data[b] = bf.data[b] | (1 << c)
	} else {
		bf.data[b] = bf.data[b] & (0xff - (1 << c))
	}
}

func (bf *Bitfield) Get(i int) bool {
	b := uint(i-(i%8)) / 8
	c := uint(7 - (i % 8))

	return (bf.data[b] & (1 << c)) != 0
}

func (bf *Bitfield) Sum() int {
	sum := 0

	for i := 0; i < bf.Length(); i++ {
		if bf.Get(i) {
			sum++
		}
	}

	return sum
}
