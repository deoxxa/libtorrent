package bitfield

import (
	"math"

	"github.com/dropbox/godropbox/container/bitvector"
)

type Bitfield struct {
	bitvector.BitVector
}

func NewBitfield(data []byte, length int) *Bitfield {
	if data == nil {
		data = make([]byte, int(math.Ceil(float64(length)/8)))
	}

	return &Bitfield{
		BitVector: *bitvector.NewBitVector(data, length),
	}
}

func (bf *Bitfield) Set(i int, v bool) {
	var n byte

	if v {
		n = 1
	} else {
		n = 0
	}

	bf.BitVector.Set(n, i)
}

func (bf *Bitfield) Get(i int) bool {
	return bf.BitVector.Element(i) == 1
}

func (bf *Bitfield) SumTrue() int {
	sum := 0

	for i := 0; i < bf.Length(); i++ {
		sum += int(bf.Element(i))
	}

	return sum
}
