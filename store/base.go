package store

import (
	"errors"

	"github.com/torrance/libtorrent/metainfo"
)

type Base struct {
	hashes      [][20]byte
	pieceLength int64
	totalLength int64
}

func NewBase(m *metainfo.Metainfo) *Base {
	b := &Base{
		hashes:      m.Pieces,
		pieceLength: m.PieceLength,
	}

	for _, file := range m.Files {
		b.totalLength += file.Length
	}

	return b
}

func (s *Base) Blocks() int {
	return len(s.hashes)
}

func (s *Base) GetSize(index int) int64 {
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

func (s *Base) GetHash(index int) (hash [20]byte, err error) {
	if len(s.hashes) <= index {
		return hash, errors.New("index is out of range")
	} else {
		return s.hashes[index], nil
	}
}

func (s *Base) GetOffset(index int) int64 {
	return s.pieceLength * int64(index)
}
