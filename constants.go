package libtorrent

const (
	STATE_STOPPED = iota
	STATE_LEARNING
	STATE_LEECHING
	STATE_SEEDING
)

var ZERO_HASH = [20]byte{
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
}
