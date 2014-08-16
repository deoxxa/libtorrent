package nullsource

import (
	"log"

	"github.com/torrance/libtorrent"
	"github.com/torrance/libtorrent/metainfo"
)

type NullSource struct {
	errors chan error
	peers  chan *libtorrent.PeerAddress
}

func NewNullSource(s *libtorrent.Session, config interface{}) (libtorrent.PeerSource, error) {
	n := &NullSource{
		errors: make(chan error),
		peers:  make(chan *libtorrent.PeerAddress),
	}

	log.Printf("new nullsource: %#v", n)

	return n, nil
}

func (s *NullSource) Metainfo(m *metainfo.Metainfo) error {
	log.Printf("metainfo provided")

	return nil
}

func (s *NullSource) Errors() chan error {
	log.Printf("errors channel requested")

	return s.errors
}

func (s *NullSource) Peers() chan *libtorrent.PeerAddress {
	log.Printf("peers channel requested")

	return s.peers
}

func (s *NullSource) Start() error {
	log.Printf("start")

	return nil
}

func (s *NullSource) Stop() error {
	log.Printf("stop")

	return nil
}

func (s *NullSource) Update() {
	log.Printf("update")
}
