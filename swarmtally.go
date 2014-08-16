package libtorrent

import (
	"github.com/facebookgo/stackerr"
	"github.com/torrance/libtorrent/bitfield"
)

type swarmTally []int

func (st swarmTally) AddBitfield(bitf *bitfield.Bitfield) error {
	if len(st) != bitf.Length() {
		return stackerr.Newf("addBitfield: Supplied bitfield incorrect size, want %d, got %d", len(st), bitf.Length())
	}

	for i := 0; i < len(st); i++ {
		if st[i] == -1 {
			continue
		}

		if bitf.Get(i) {
			st[i]++
		}
	}

	return nil
}

func (st swarmTally) AddIndex(i int) error {
	if i >= len(st) {
		return stackerr.Newf("addIndex: Supplied index too big, want <= %d, got %d", len(st), i)
	}

	if st[i] != -1 {
		st[i]++
	}

	return nil
}

func (st swarmTally) RemoveBitfield(bitf *bitfield.Bitfield) error {
	if len(st) != bitf.Length() {
		return stackerr.Newf("removeBitfield: Supplied bitfield incorrect size, want %d, got %d", len(st), bitf.Length())
	}

	for i := 0; i < len(st); i++ {
		if st[i] <= 0 {
			continue
		}

		if bitf.Get(i) {
			st[i]--
		}
	}

	return nil
}

func (st swarmTally) RemoveIndex(i int) error {
	if i >= len(st) {
		return stackerr.Newf("removeIndex: Supplied index too big, want <= %d, got %d", len(st), i)
	}

	if st[i] > 0 {
		st[i]--
	}

	return nil
}

func (st swarmTally) Zero() {
	for i := 0; i < len(st); i++ {
		if st[i] != -1 {
			st[i] = 0
		}
	}
}
