package trackers

import (
	"net/url"

	"github.com/facebookgo/stackerr"
	"github.com/torrance/libtorrent"
	"github.com/torrance/libtorrent/metainfo"
	"github.com/torrance/libtorrent/tracker"
)

type Config struct {
	Transports map[string]tracker.TransportFactory
}

type Trackers struct {
	session  *libtorrent.Session
	config   Config
	errors   chan error
	peers    chan *libtorrent.PeerAddress
	trackers []*tracker.Tracker
	started  bool
}

func NewTrackers(s *libtorrent.Session, config interface{}) (libtorrent.PeerSource, error) {
	tconfig, ok := config.(Config)
	if !ok {
		return nil, stackerr.New("invalid config type")
	}

	t := &Trackers{
		session: s,
		config:  tconfig,
		errors:  make(chan error, 50),
		peers:   make(chan *libtorrent.PeerAddress, 50),
		started: false,
	}

	return t, nil
}

func (s *Trackers) Metainfo(m *metainfo.Metainfo) error {
	for _, trackerUrl := range m.AnnounceList {
		u, err := url.Parse(trackerUrl)
		if err != nil {
			s.errors <- stackerr.Wrap(err)
			continue
		}

		transportFactory, ok := s.config.Transports[u.Scheme]
		if !ok {
			s.errors <- stackerr.Newf("unrecognised tracker scheme %s for %s", u.Scheme, trackerUrl)
			continue
		}

		transport, err := transportFactory.Constructor(u, transportFactory.Config)
		if err != nil {
			s.errors <- stackerr.Wrap(err)
			continue
		}

		t, err := tracker.NewTracker(transport, s.session)
		if err != nil {
			s.errors <- stackerr.Wrap(err)
			continue
		}

		s.trackers = append(s.trackers, t)

		go func() {
			for peer := range t.Peers() {
				s.peers <- peer
			}
		}()

		go func() {
			for err := range t.Errors() {
				s.errors <- stackerr.Wrap(err)
			}
		}()

		if s.started {
			t.Start()
		}
	}

	return nil
}

func (s *Trackers) Errors() chan error {
	return s.errors
}

func (s *Trackers) Peers() chan *libtorrent.PeerAddress {
	return s.peers
}

func (s *Trackers) Start() error {
	s.started = true

	for _, tracker := range s.trackers {
		tracker.Start()
	}

	return nil
}

func (s *Trackers) Stop() error {
	s.started = false

	for _, tracker := range s.trackers {
		tracker.Stop()
	}

	return nil
}

func (s *Trackers) Update() {
	for _, tracker := range s.trackers {
		tracker.Update()
	}
}
