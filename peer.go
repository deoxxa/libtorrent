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

	kill chan bool

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
	metadataQueue   c3.Queue
	metadataRunning c3.Bag

	uploadOnly bool
}

func NewPeer(addr string, hs *handshake, conn io.ReadWriter) (p *Peer) {
	p = &Peer{
		Errors:   make(chan error, 100),
		Incoming: make(chan interface{}, 10),
		Outgoing: make(chan binaryDumper, 10),

		kill: make(chan bool, 1),

		addr: addr,
		conn: conn,

		peerId: hs.peerId,

		hasExtensions:  hs.flags.Get(43),
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

		metadataQueue:   c3.NewQueue(),
		metadataRunning: c3.NewBag(),
	}

	go p.readMessages()
	go p.writeMessages()

	return
}

func (p *Peer) Addr() string {
	return p.addr
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

func (p *Peer) SetPeerChoking(b bool) (changed bool) {
	p.mutex.Lock()
	changed = p.peerChoking != b
	p.peerChoking = b
	p.mutex.Unlock()
	return
}

func (p *Peer) GetPeerInterested() (b bool) {
	p.mutex.RLock()
	b = p.peerInterested
	p.mutex.RUnlock()
	return
}

func (p *Peer) SetPeerInterested(b bool) (changed bool) {
	p.mutex.Lock()
	changed = p.peerInterested != b
	p.peerInterested = b
	p.mutex.Unlock()
	return
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

func (p *Peer) readMessages() {
	for {
		if msg, err := parsePeerMessage(p, p.conn); err != nil {
			e := stackerr.Underlying(err)
			if e != nil && e[len(e)-1] == io.EOF {
				break
			}

			p.Errors <- stackerr.Wrap(err)
		} else {
			p.Incoming <- msg
		}
	}

	p.kill <- true

	close(p.Errors)
	close(p.Incoming)
}

func (p *Peer) writeMessages() {
LOOP:
	for {
		var msg binaryDumper

		select {
		case _msg := <-p.Outgoing:
			msg = _msg
		case <-time.After(time.Second * 30):
			msg = new(keepaliveMessage)
		case <-p.kill:
			break LOOP
		}

		if err := msg.BinaryDump(p, p.conn); err != nil {
			p.Errors <- stackerr.Wrap(err)
		}
	}

	close(p.Outgoing)
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

func (p *Peer) maybeSendMetadataRequests() {
	p.mutex.Lock()

	for {
		if p.metadataRunning.Len() >= 3 {
			break
		}

		rr, ok := p.metadataQueue.Dequeue()
		if !ok {
			break
		}

		r, ok := rr.(*extendedUtMetadataMessage)
		if !ok {
			break
		}

		p.metadataRunning.Add(*r)

		p.Outgoing <- r
	}

	p.mutex.Unlock()
}

func (p *Peer) maybeCancelMetadataRequests() {
	p.mutex.Lock()

	p.metadataQueue.Clear()
	p.metadataPieces = nil
	p.metadataContent = nil

	p.mutex.Unlock()
}
