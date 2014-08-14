package store

import (
	"bytes"
	"crypto/sha1"

	"github.com/torrance/libtorrent/bitfield"
	"github.com/torrance/libtorrent/metainfo"
)

type Constructor func(m *metainfo.Metainfo, config interface{}) (store Store, err error)

type Factory struct {
	Constructor Constructor
	Config      interface{}
}

type Store interface {
	Blocks() int
	GetSize(index int) int64
	GetHash(index int) (hash [20]byte, err error)
	GetOffset(index int) int64
	GetBlock(index int, offset int64, block []byte) (n int, err error)
	SetBlock(index int, offset int64, block []byte) (n int, err error)
}

func ValidateBlock(s Store, index int) (ok bool, err error) {
	block := make([]byte, s.GetSize(index))
	if _, err := s.GetBlock(index, 0, block); err != nil {
		return false, err
	}

	hash, err := s.GetHash(index)
	if err != nil {
		return false, err
	}

	h := sha1.New()
	h.Write(block)

	return bytes.Equal(h.Sum(nil), hash[:]), nil
}

func Validate(s Store) (bf *bitfield.Bitfield, err error) {
	bf = bitfield.NewBitfield(s.Blocks())

	for i := 0; i < s.Blocks(); i++ {
		if ok, err := ValidateBlock(s, i); err != nil {
			return nil, err
		} else if ok {
			bf.SetTrue(i)
		}
	}

	return bf, nil
}
