package libtorrent

import (
	"bytes"
	"crypto/sha1"

	"github.com/facebookgo/stackerr"
)

type StoreConstructor func(m *Metainfo, config interface{}) (store Store, err error)

type StoreFactory struct {
	Constructor StoreConstructor
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
		return false, stackerr.Wrap(err)
	}

	hash, err := s.GetHash(index)
	if err != nil {
		return false, stackerr.Wrap(err)
	}

	h := sha1.New()
	h.Write(block)

	return bytes.Equal(h.Sum(nil), hash[:]), nil
}

func Validate(s Store) (*Bitfield, error) {
	bv := NewBitfield(nil, s.Blocks())

	for i := 0; i < s.Blocks(); i++ {
		if ok, err := ValidateBlock(s, i); err != nil {
			return nil, stackerr.Wrap(err)
		} else if ok {
			bv.Set(i, true)
		}
	}

	return bv, nil
}

type BaseStore struct {
	hashes      [][20]byte
	pieceLength int64
	totalLength int64
}

func NewBaseStore(m *Metainfo) *BaseStore {
	b := &BaseStore{
		hashes:      m.Pieces,
		pieceLength: m.PieceLength,
	}

	for _, file := range m.Files {
		b.totalLength += file.Length
	}

	return b
}

func (s *BaseStore) Blocks() int {
	return len(s.hashes)
}

func (s *BaseStore) GetSize(index int) int64 {
	if index == len(s.hashes)-1 {
		l := s.totalLength % s.pieceLength

		if l == 0 {
			return s.pieceLength
		} else {
			return l
		}
	} else {
		return s.pieceLength
	}
}

func (s *BaseStore) GetHash(index int) (hash [20]byte, err error) {
	if len(s.hashes) <= index {
		return hash, stackerr.New("index is out of range")
	} else {
		return s.hashes[index], nil
	}
}

func (s *BaseStore) GetOffset(index int) int64 {
	return s.pieceLength * int64(index)
}
