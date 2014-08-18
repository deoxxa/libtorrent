package libtorrent

type Config struct {
	InfoHash          [20]byte
	PeerId            [20]byte
	IP                [4]byte
	Port              uint16
	Name              string
	StoreFactory      StoreFactory
	PeerSources       []PeerSourceFactory
	TrackerTransports map[string]TrackerTransportFactory
	Trackers          []string
}
