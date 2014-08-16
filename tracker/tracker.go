package tracker

import (
	"net/url"
	"time"

	"github.com/facebookgo/stackerr"
	"github.com/torrance/libtorrent"
)

type TransportFactory struct {
	Constructor func(u *url.URL, config interface{}) (Transport, error)
	Config      interface{}
}

type Trackable interface {
	InfoHash() [20]byte
	PeerId() [20]byte
	IP() [4]byte
	Port() uint16
	Uploaded() int64
	Downloaded() int64
	Left() int64
}

type Event byte

const (
	EVENT_NONE = Event(iota)
	EVENT_COMPLETED
	EVENT_STARTED
	EVENT_STOPPED
)

type AnnounceRequest struct {
	Event      Event
	InfoHash   [20]byte
	PeerId     [20]byte
	IP         [4]byte
	Port       uint16
	Uploaded   int64
	Downloaded int64
	Left       int64
}

type AnnounceResponse struct {
	Leechers          uint32
	Seeders           uint32
	Peers             []*libtorrent.PeerAddress
	SuggestedInterval time.Duration
	RequiredInterval  time.Duration
}

type Transport interface {
	Announce(req *AnnounceRequest) (*AnnounceResponse, error)
}

type Tracker struct {
	errors chan error
	peers  chan *libtorrent.PeerAddress

	transport Transport
	subject   Trackable

	force chan bool
	stop  chan bool
	n     uint
	next  time.Duration
}

func (t *Tracker) Errors() chan error {
	return t.errors
}

func (t *Tracker) Peers() chan *libtorrent.PeerAddress {
	return t.peers
}

func (t *Tracker) Update() {
	t.force <- true
}

func NewTracker(transport Transport, subject Trackable) (*Tracker, error) {
	t := &Tracker{
		errors:    make(chan error, 50),
		peers:     make(chan *libtorrent.PeerAddress, 50),
		transport: transport,
		subject:   subject,
		force:     make(chan bool, 50),
		stop:      make(chan bool, 50),
	}

	return t, nil
}

func (t *Tracker) Start() {
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

			areq := &AnnounceRequest{
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

			t.next = time.Second * ares.SuggestedInterval

			event = EVENT_NONE

			for _, peer := range ares.Peers {
				t.peers <- peer
			}
		}

		sreq := &AnnounceRequest{
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
}

func (t *Tracker) Stop() {
	t.stop <- true
}
