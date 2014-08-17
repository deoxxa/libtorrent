package libtorrent

import (
	"io"
	"math"
	"sync"
	"time"

	"github.com/ReSc/c3"
	"github.com/facebookgo/stackerr"
)

type Peer struct {
	Errors   chan error
	Incoming chan interface{}
	Outgoing chan binaryDumper

	addr string
	conn io.ReadWriter

	peerId   [20]byte
	reserved [8]byte

	hasExtensions  bool
	extensionIds   map[string]int
	extensionNames map[int]string
	version        string
	reportedIp     string
	reportedIpv4   [4]byte
	reportedIpv6   [16]byte

	amChoking      bool
	amInterested   bool
	peerChoking    bool
	peerInterested bool

	mutex sync.RWMutex

	blocks *Bitfield

	maxRequests     int
	requestingBlock int
	requestQueue    c3.Queue
	requestsRunning c3.Bag

	metadataContent []byte
	metadataPieces  *Bitfield
}

func NewPeer(addr string, hs *handshake, conn io.ReadWriter) (p *Peer) {
	p = &Peer{
		Errors:   make(chan error, 100),
		Incoming: make(chan interface{}, 10),
		Outgoing: make(chan binaryDumper, 10),

		addr: addr,
		conn: conn,

		peerId: hs.peerId,

		hasExtensions:  hs.flags.Get(44),
		extensionIds:   map[string]int{},
		extensionNames: map[int]string{},

		amChoking:      true,
		amInterested:   false,
		peerChoking:    true,
		peerInterested: false,

		maxRequests:     75,
		requestingBlock: -1,
		requestQueue:    c3.NewQueue(),
		requestsRunning: c3.NewBag(),
	}

	go p.readMessages()
	go p.writeMessages()

	return
}

func (p *Peer) readMessages() {
	for {
		if msg, err := parsePeerMessage(p, p.conn); err != nil {
			p.Errors <- stackerr.Wrap(err)
		} else {
			p.Incoming <- msg
		}
	}
}

func (p *Peer) writeMessages() {
	for {
		var msg binaryDumper

		select {
		case _msg := <-p.Outgoing:
			msg = _msg
		case <-time.After(time.Second * 30):
			msg = new(keepaliveMessage)
		}

		if err := msg.BinaryDump(p, p.conn); err != nil {
			p.Errors <- stackerr.Wrap(err)
		}
	}
}

func (p *Peer) GetAmChoking() (b bool) {
	p.mutex.RLock()
	b = p.amChoking
	p.mutex.RUnlock()
	return
}

func (p *Peer) SetAmChoking(b bool) (changed bool) {
	p.mutex.Lock()
	changed = p.amChoking != b
	p.amChoking = b
	p.mutex.Unlock()
	return
}

func (p *Peer) GetAmInterested() (b bool) {
	p.mutex.RLock()
	b = p.amInterested
	p.mutex.RUnlock()
	return
}

func (p *Peer) SetAmInterested(b bool) (changed bool) {
	p.mutex.Lock()
	changed = p.amInterested != b
	p.amInterested = b
	p.mutex.Unlock()
	return
}

func (p *Peer) GetPeerChoking() (b bool) {
	p.mutex.RLock()
	b = p.peerChoking
	p.mutex.RUnlock()
	return
}

func (p *Peer) SetPeerChoking(b bool) {
	p.mutex.Lock()
	p.peerChoking = b
	p.mutex.Unlock()
}

func (p *Peer) GetPeerInterested() (b bool) {
	p.mutex.RLock()
	b = p.peerInterested
	p.mutex.RUnlock()
	return
}

func (p *Peer) SetPeerInterested(b bool) {
	p.mutex.Lock()
	p.peerInterested = b
	p.mutex.Unlock()
}

func (p *Peer) SetBitfield(blocks *Bitfield) {
	p.mutex.Lock()
	p.blocks = blocks
	p.mutex.Unlock()
}

func (p *Peer) MarkPieceComplete(index int) {
	p.mutex.Lock()

	if p.blocks == nil {
		p.blocks = NewBitfield(nil, index)
	}

	if p.blocks.Length() < index+1 {
		d := make([]byte, int(math.Ceil(float64(index+1)/8)))
		copy(d, p.blocks.Bytes())
		p.blocks = NewBitfield(d, index+1)
	}

	p.blocks.Set(index, true)

	p.mutex.Unlock()
}

func (p *Peer) maybeSendPieceRequests() {
	p.mutex.Lock()

	for {
		if p.requestsRunning.Len() >= p.maxRequests {
			break
		}

		rr, ok := p.requestQueue.Dequeue()
		if !ok {
			break
		}

		r, ok := rr.(*requestMessage)
		if !ok {
			break
		}

		p.requestsRunning.Add(*r)

		p.Outgoing <- r
	}

	p.mutex.Unlock()
}
