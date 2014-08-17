package libtorrent

type PeerAddress struct {
	Host string
	Port uint16
}

type PeerSourceFactory struct {
	Constructor func(s *Session, config interface{}) (PeerSource, error)
	Config      interface{}
}

type PeerSource interface {
	Metainfo(m *Metainfo) error
	Errors() chan error
	Peers() chan *PeerAddress
	Start() error
	Stop() error
	ForceUpdate()
}
