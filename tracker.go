package libtorrent

import (
	"net/url"
	"time"

	"github.com/facebookgo/stackerr"
)

type TrackerTransportFactory struct {
	Constructor func(u *url.URL, config interface{}) (TrackerTransport, error)
	Config      interface{}
}

type Trackable interface {
	InfoHash() [20]byte
	PeerId() [20]byte
	IP() [4]byte
	Port() uint16
	Uploaded() uint64
	Downloaded() uint64
	Left() uint64
}

type Event byte

const (
	EVENT_NONE = Event(iota)
	EVENT_COMPLETED
	EVENT_STARTED
	EVENT_STOPPED
)

type TrackerAnnounceRequest struct {
	Event      Event
	InfoHash   [20]byte
	PeerId     [20]byte
	IP         [4]byte
	Port       uint16
	Uploaded   uint64
	Downloaded uint64
	Left       uint64
}

type TrackerAnnounceResponse struct {
	Leechers          uint32
	Seeders           uint32
	Peers             []*PeerAddress
	SuggestedInterval time.Duration
	RequiredInterval  time.Duration
}

type TrackerTransport interface {
	Announce(req *TrackerAnnounceRequest) (*TrackerAnnounceResponse, error)
}

type Tracker struct {
	errors chan error
	peers  chan *PeerAddress

	transport TrackerTransport
	subject   Trackable

	force chan bool
	stop  chan bool
	n     uint
	next  time.Duration
}

func (t *Tracker) Metainfo(metainfo *Metainfo) error {
	return nil
}

func (t *Tracker) Errors() chan error {
	return t.errors
}

func (t *Tracker) Peers() chan *PeerAddress {
	return t.peers
}

func (t *Tracker) ForceUpdate() {
	t.force <- true
}

func NewTracker(transport TrackerTransport, subject Trackable) (*Tracker, error) {
	t := &Tracker{
		errors:    make(chan error, 50),
		peers:     make(chan *PeerAddress, 50),
		transport: transport,
		subject:   subject,
		force:     make(chan bool, 50),
		stop:      make(chan bool, 50),
	}

	return t, nil
}

func (t *Tracker) Start() error {
	t.next = 0

	event := EVENT_STARTED

	go func() {
	L:
		for {
			select {
			case <-time.After(t.next):
			case <-t.force:
			case <-t.stop:
				break L
			}

			areq := &TrackerAnnounceRequest{
				Event:      event,
				InfoHash:   t.subject.InfoHash(),
				PeerId:     t.subject.PeerId(),
				IP:         t.subject.IP(),
				Port:       t.subject.Port(),
				Uploaded:   t.subject.Uploaded(),
				Downloaded: t.subject.Downloaded(),
				Left:       t.subject.Left(),
			}

			ares, err := t.transport.Announce(areq)
			if err != nil {
				t.errors <- stackerr.Wrap(err)

				t.next = time.Second * 60 * time.Duration(1<<t.n)
				t.n++

				continue
			}

			t.next = ares.SuggestedInterval

			event = EVENT_NONE

			for _, peer := range ares.Peers {
				t.peers <- peer
			}
		}

		sreq := &TrackerAnnounceRequest{
			Event:      EVENT_STOPPED,
			InfoHash:   t.subject.InfoHash(),
			PeerId:     t.subject.PeerId(),
			IP:         t.subject.IP(),
			Port:       t.subject.Port(),
			Uploaded:   t.subject.Uploaded(),
			Downloaded: t.subject.Downloaded(),
			Left:       t.subject.Left(),
		}

		if _, err := t.transport.Announce(sreq); err != nil {
			t.errors <- stackerr.Wrap(err)
		}
	}()

	return nil
}

func (t *Tracker) Stop() error {
	t.stop <- true

	return nil
}
