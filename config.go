package libtorrent

import (
	"github.com/torrance/libtorrent/store"
)

type Config struct {
	Port         uint16
	PeerId       [20]byte
	StoreFactory store.Factory
}
