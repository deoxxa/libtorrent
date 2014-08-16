package libtorrent

import (
	"github.com/torrance/libtorrent/store"
)

type Config struct {
	InfoHash            [20]byte
	PeerId              [20]byte
	IP                  [4]byte
	Port                uint16
	Name                string
	StoreFactory        store.Factory
	PeerSourceFactories []PeerSourceFactory
}
