package libtorrent

type Config struct {
	InfoHash            [20]byte
	PeerId              [20]byte
	IP                  [4]byte
	Port                uint16
	Name                string
	StoreFactory        StoreFactory
	PeerSourceFactories []PeerSourceFactory
}
