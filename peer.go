package libtorrent

import (
	"io"
	"sync"
	"time"
	//"testing/iotest"

	"github.com/ReSc/c3"
	"github.com/torrance/libtorrent/bitfield"
)

type peer struct {
	peerId        [20]byte
	reserved      [8]byte
	hasExtensions bool
	extensions    map[int]string

	amChoking      bool
	amInterested   bool
	peerChoking    bool
	peerInterested bool

	conn  io.ReadWriter
	write chan binaryDumper
	read  chan peerDouble

	mutex sync.RWMutex

	blocks *bitfield.Bitfield

	maxRequests     int
	requestingBlock int
	requestQueue    c3.Queue
	requestsRunning c3.Bag
}

type peerDouble struct {
	msg  interface{}
	peer *peer
}

func newPeer(hs *handshake, conn io.ReadWriter, readChan chan peerDouble) (p *peer) {
	p = &peer{
		peerId:        hs.peerId,
		hasExtensions: hs.flags.Element(20) == 1,
		extensions:    map[int]string{},

		amChoking:      true,
		amInterested:   false,
		peerChoking:    true,
		peerInterested: false,

		conn:  conn,
		write: make(chan binaryDumper, 10),
		read:  readChan,

		maxRequests:     25,
		requestingBlock: -1,
		requestQueue:    c3.NewQueue(),
		requestsRunning: c3.NewBag(),
	}

	// Write loop
	go func() {
		for {
			//conn := iotest.NewWriteLogger("Writing", conn)
			var msg binaryDumper

			select {
			case _msg := <-p.write:
				msg = _msg
			case <-time.After(time.Second * 5):
				msg = new(keepaliveMessage)
			}

			if err := msg.BinaryDump(conn); err != nil {
				logger.Error(err.Error())
				break
			}
		}
	}()

	// Read loop
	go func() {
		for {
			//conn := iotest.NewReadLogger("Reading", conn)
			msg, err := parsePeerMessage(conn)

			if _, ok := err.(unknownMessage); ok {
				// Log unknown messages and then ignore
				logger.Info(err.Error())
			} else if err != nil {
				// TODO: Close peer
				logger.Debug("%s Received error reading connection: %s", p.peerId, err)
				break
			}

			readChan <- peerDouble{msg: msg, peer: p}
		}
	}()

	return
}

func (p *peer) GetAmChoking() (b bool) {
	p.mutex.RLock()
	b = p.amChoking
	p.mutex.RUnlock()
	return
}

func (p *peer) SetAmChoking(b bool) (changed bool) {
	p.mutex.Lock()
	changed = p.amChoking != b
	p.amChoking = b
	p.mutex.Unlock()
	return
}

func (p *peer) GetAmInterested() (b bool) {
	p.mutex.RLock()
	b = p.amInterested
	p.mutex.RUnlock()
	return
}

func (p *peer) SetAmInterested(b bool) (changed bool) {
	p.mutex.Lock()
	changed = p.amInterested != b
	p.amInterested = b
	p.mutex.Unlock()
	return
}

func (p *peer) GetPeerChoking() (b bool) {
	p.mutex.RLock()
	b = p.peerChoking
	p.mutex.RUnlock()
	return
}

func (p *peer) SetPeerChoking(b bool) {
	p.mutex.Lock()
	p.peerChoking = b
	p.mutex.Unlock()
}

func (p *peer) GetPeerInterested() (b bool) {
	p.mutex.RLock()
	b = p.peerInterested
	p.mutex.RUnlock()
	return
}

func (p *peer) SetPeerInterested(b bool) {
	p.mutex.Lock()
	p.peerInterested = b
	p.mutex.Unlock()
}

func (p *peer) SetBitfield(blocks *bitfield.Bitfield) {
	p.mutex.Lock()
	p.blocks = blocks
	p.mutex.Unlock()
}

func (p *peer) HasPiece(index int) {
	p.mutex.Lock()
	p.blocks.Set(index, true)
	p.mutex.Unlock()
}

func (p *peer) MaybeSendPieceRequests() {
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

		p.write <- r
	}

	p.mutex.Unlock()
}
