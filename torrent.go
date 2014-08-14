package libtorrent

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/op/go-logging"
	"github.com/torrance/libtorrent/bitfield"
	"github.com/torrance/libtorrent/metainfo"
	"github.com/torrance/libtorrent/store"
	"github.com/torrance/libtorrent/tracker"
)

const (
	Stopped = iota
	Leeching
	Seeding
)

var logger = logging.MustGetLogger("libtorrent")

type Torrent struct {
	peerId           [20]byte
	meta             *metainfo.Metainfo
	store            store.Store
	config           *Config
	bf               *bitfield.Bitfield
	swarm            []*peer
	incomingPeer     chan *peer
	incomingPeerAddr chan string
	requesting       *bitfield.Bitfield
	swarmTally       swarmTally
	readChan         chan peerDouble
	trackers         []*tracker.Tracker
	state            int
	stateLock        sync.Mutex
}

func NewTorrent(m *metainfo.Metainfo, config *Config) (*Torrent, error) {
	t := &Torrent{
		config:           config,
		meta:             m,
		incomingPeer:     make(chan *peer, 100),
		incomingPeerAddr: make(chan string, 100),
		readChan:         make(chan peerDouble, 50),
		state:            Stopped,
	}

	if store, err := config.StoreFactory.Constructor(m, config.StoreFactory.Config); err != nil {
		return nil, err
	} else {
		t.store = store
	}

	if bf, err := store.Validate(t.store); err != nil {
		return nil, err
	} else {
		t.bf = bf
	}

	t.requesting = bitfield.NewBitfield(t.bf.Length())

	return t, nil
}

func (t *Torrent) Start() {
	logger.Info("Torrent starting: %s", t.meta.Name)

	// Set initial state
	t.stateLock.Lock()
	if t.bf.SumTrue() == t.bf.Length() {
		t.state = Seeding
	} else {
		t.state = Leeching
	}
	t.stateLock.Unlock()

	// Create trackers
	for _, tkr := range t.meta.AnnounceList {
		tkr, err := tracker.NewTracker(tkr, t, t.incomingPeerAddr)
		if err != nil {
			logger.Error(err.Error())
			continue
		}
		t.trackers = append(t.trackers, tkr)
		tkr.Start()
	}

	// Tracker loop
	go func() {
		for {
			peerAddr := <-t.incomingPeerAddr
			// Only attempt to connect to other peers whilst leeching
			if t.state != Leeching {
				continue
			}
			go func() {
				conn, err := net.Dial("tcp", peerAddr)
				if err != nil {
					logger.Debug("Failed to connect to tracker peer address %s: %s", peerAddr, err)
					return
				}
				t.AddPeer(conn, nil)
			}()
		}
	}()

	// Peer loop
	go func() {
		for {
			select {
			case peer := <-t.incomingPeer:
				// Add to swarm slice
				logger.Debug("Connected to new peer: %s", peer.name)
				t.swarm = append(t.swarm, peer)
			case <-time.After(time.Second * 5):
				// Unchoke interested peers
				// TODO: Implement maximum unchoked peers
				// TODO: Implement optimistic unchoking algorithm
				for _, peer := range t.swarm {
					if peer.GetPeerInterested() && peer.GetAmChoking() {
						logger.Debug("Unchoking peer %s", peer.name)

						if peer.SetAmChoking(false) {
							peer.write <- &unchokeMessage{}
						}
					}
				}
			}
		}
	}()

	// Receive loop
	go func() {
		for {
			peerDouble := <-t.readChan
			peer := peerDouble.peer
			msg := peerDouble.msg

			switch msg := msg.(type) {
			case *keepaliveMessage:
				logger.Debug("Peer %s sent a keepalive message", peer.name)
			case *chokeMessage:
				logger.Debug("Peer %s has choked us", peer.name)
				peer.SetPeerChoking(true)
			case *unchokeMessage:
				logger.Debug("Peer %s has unchoked us", peer.name)
				peer.SetPeerChoking(false)
				t.MaybeRequestPieces(peer)
			case *interestedMessage:
				logger.Debug("Peer %s has said it is interested", peer.name)
				peer.SetPeerInterested(true)
			case *uninterestedMessage:
				logger.Debug("Peer %s has said it is uninterested", peer.name)
				peer.SetPeerInterested(false)
			case *haveMessage:
				pieceIndex := int(msg.pieceIndex)
				logger.Debug("Peer %s has piece %d", peer.name, pieceIndex)
				if pieceIndex >= t.meta.PieceCount {
					logger.Debug("Peer %s sent an out of range have message")
					// TODO: Shutdown client
					break
				}
				peer.HasPiece(pieceIndex)
				t.swarmTally.AddIndex(pieceIndex)
				t.MaybeRequestPieces(peer)
			case *bitfieldMessage:
				logger.Debug("Peer %s has sent us its bitfield", peer.name)
				// Raw parsed bitfield has no actual length. Let's try to set it.
				if err := msg.bf.SetLength(t.meta.PieceCount); err != nil {
					logger.Error(err.Error())
					// TODO: Shutdown client
					break
				}
				peer.SetBitfield(msg.bf)
				t.swarmTally.AddBitfield(msg.bf)
				t.MaybeRequestPieces(peer)
			case *requestMessage:
				if peer.GetAmChoking() || !t.bf.Get(int(msg.pieceIndex)) || msg.blockLength > 32768 {
					logger.Debug("Peer %s has asked for a block (%d, %d, %d), but we are rejecting them", peer.name, msg.pieceIndex, msg.blockOffset, msg.blockLength)
					// TODO: Add naughty points
					break
				}

				logger.Debug("Peer %s has asked for a block (%d, %d, %d), going to fetch block", peer.name, msg.pieceIndex, msg.blockOffset, msg.blockLength)

				block := make([]byte, msg.blockLength)
				if _, err := t.store.GetBlock(int(msg.pieceIndex), int64(msg.blockOffset), block); err != nil {
					logger.Error(err.Error())
					break
				}

				logger.Debug("Peer %s has asked for a block (%d, %d, %d), sending it to them", peer.name, msg.pieceIndex, msg.blockOffset, msg.blockLength)

				peer.write <- &pieceMessage{
					pieceIndex:  msg.pieceIndex,
					blockOffset: msg.blockOffset,
					data:        block,
				}
			case *pieceMessage:
				valid := false
				for i, request := range peer.requestsRunning {
					if request.pieceIndex == msg.pieceIndex && request.blockOffset == msg.blockOffset && int(request.blockLength) == len(msg.data) {
						valid = true
						copy(peer.requestsRunning[i:], peer.requestsRunning[i+1:])
						peer.requestsRunning[len(peer.requestsRunning)-1] = nil
						peer.requestsRunning = peer.requestsRunning[:len(peer.requestsRunning)-1]
						break
					}
				}

				if !valid {
					logger.Debug("Peer %s sent us a piece (%d, %d - %d) that we don't have a corresponding request for", peer.name, msg.pieceIndex, msg.blockOffset, int(msg.blockOffset)+len(msg.data))
				} else {
					logger.Debug("Peer %s sent us a piece (%d, %d - %d)", peer.name, msg.pieceIndex, msg.blockOffset, int(msg.blockOffset)+len(msg.data))

					if _, err := t.store.SetBlock(int(msg.pieceIndex), int64(msg.blockOffset), msg.data); err != nil {
						logger.Error(err.Error())
						break
					}
				}

				if len(peer.requests)+len(peer.requestsRunning) == 0 {
					if ok, err := store.ValidateBlock(t.store, peer.requesting); err == nil {
						if ok {
							logger.Debug("Block %d completed, marking as done", peer.requesting)

							t.bf.SetTrue(peer.requesting)
						} else {
							logger.Debug("Block %d failed hash check, retrying", peer.requesting)
						}
					}

					peer.requesting = -1
					t.MaybeRequestPieces(peer)
				} else {
					peer.MaybeSendPieceRequests()
				}
			// case *cancelMessage:
			default:
				logger.Debug("Peer %s sent unknown message", peer.name)
			}
		}
	}()
}

func (t *Torrent) String() string {
	s := `Torrent: %x
    Name: '%s'
    Piece length: %d
    Announce lists: %v`
	return fmt.Sprintf(s, t.meta.InfoHash, t.meta.Name, t.meta.PieceLength, t.meta.AnnounceList)
}

func (t *Torrent) Name() string {
	return t.meta.Name
}

func (t *Torrent) InfoHash() [20]byte {
	return t.meta.InfoHash
}

func (t *Torrent) State() (state int) {
	t.stateLock.Lock()
	state = t.state
	t.stateLock.Unlock()
	return
}

func (t *Torrent) AddPeer(conn net.Conn, hs *handshake) {
	// Set 60 second limit to connection attempt
	conn.SetDeadline(time.Now().Add(time.Minute))

	// Send handshake
	if err := newHandshake(t.InfoHash(), t.PeerId()).BinaryDump(conn); err != nil {
		logger.Debug("%s Failed to send handshake to connection: %s", conn.RemoteAddr(), err)
		return
	}

	// If hs is nil, this means we've attempted to establish the connection and need to wait
	// for their handshake in response
	var err error
	if hs == nil {
		if hs, err = parseHandshake(conn); err != nil {
			logger.Debug("%s Failed to parse incoming handshake: %s", conn.RemoteAddr(), err)
			return
		} else if hs.infoHash != t.InfoHash() {
			logger.Debug("%s Infohash did not match for connection", conn.RemoteAddr())
			return
		}
	}

	peer := newPeer(string(hs.peerId[:]), conn, t.readChan)
	peer.write <- &bitfieldMessage{bf: t.bf}
	t.incomingPeer <- peer

	conn.SetDeadline(time.Time{})
}

func (t *Torrent) Downloaded() int64 {
	// TODO:
	return 0
}

func (t *Torrent) Uploaded() int64 {
	// TODO:
	return 0
}

func (t *Torrent) Left() int64 {
	// TODO:
	var r int64

	for _, f := range t.meta.Files {
		r += f.Length
	}

	return r
}

func (t *Torrent) Port() uint16 {
	return t.config.Port
}

func (t *Torrent) PeerId() [20]byte {
	return t.peerId
}

func (t *Torrent) MaybeRequestPieces(p *peer) {
	if p.bf == nil {
		return
	}

	for i := 0; i < t.bf.Length(); i++ {
		if t.bf.Get(i) || t.requesting.Get(i) || !p.bf.Get(i) {
			continue
		}

		if p.SetAmInterested(true) {
			p.write <- &interestedMessage{}
		}

		if p.GetPeerChoking() {
			break
		}

		if p.requesting != -1 {
			break
		}

		t.requesting.SetTrue(i)
		p.requesting = i

		for o := 0; o < int(t.meta.PieceLength/8192); o++ {
			p.requests = append(p.requests, &requestMessage{
				pieceIndex:  uint32(i),
				blockOffset: uint32(o * 8192),
				blockLength: 8192,
			})
		}

		p.MaybeSendPieceRequests()
	}
}
