package libtorrent

import (
	"io"
	"sync"
	"time"
	//"testing/iotest"

	"github.com/torrance/libtorrent/bitfield"
)

type peer struct {
	name            string
	conn            io.ReadWriter
	write           chan binaryDumper
	read            chan peerDouble
	amChoking       bool
	amInterested    bool
	peerChoking     bool
	peerInterested  bool
	mutex           sync.RWMutex
	bf              *bitfield.Bitfield
	requesting      int
	requests        []*requestMessage
	requestsRunning []*requestMessage
}

type peerDouble struct {
	msg  interface{}
	peer *peer
}

func newPeer(name string, conn io.ReadWriter, readChan chan peerDouble) (p *peer) {
	p = &peer{
		name:           name,
		conn:           conn,
		write:          make(chan binaryDumper, 10),
		read:           readChan,
		amChoking:      true,
		amInterested:   false,
		peerChoking:    true,
		peerInterested: false,
		requesting:     -1,
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

			logger.Info("sending message %#v to %s", msg, p.name)

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
				logger.Debug("%s Received error reading connection: %s", p.name, err)
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

func (p *peer) SetBitfield(bf *bitfield.Bitfield) {
	p.mutex.Lock()
	p.bf = bf
	p.mutex.Unlock()
}

func (p *peer) HasPiece(index int) {
	p.mutex.Lock()
	p.bf.SetTrue(index)
	p.mutex.Unlock()
}

func (p *peer) MaybeSendPieceRequests() {
	p.mutex.Lock()

	for i := len(p.requestsRunning); i < 10; i++ {
		if len(p.requests) == 0 {
			break
		}

		p.write <- p.requests[0]

		p.requestsRunning = append(p.requestsRunning, p.requests[0])

		p.requests[0] = nil
		p.requests = p.requests[1:]
	}

	p.mutex.Unlock()
}
