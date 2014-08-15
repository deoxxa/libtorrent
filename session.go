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
	STATE_STOPPED = iota
	STATE_LEARNING
	STATE_LEECHING
	STATE_SEEDING
)

var ZERO_HASH = [20]byte{
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
}

var logger = logging.MustGetLogger("libtorrent")

type Session struct {
	config *Config

	peerId   [20]byte
	infoHash [20]byte
	name     string

	state     int
	stateLock sync.Mutex

	trackers         []*tracker.Tracker
	incomingPeer     chan *peer
	incomingPeerAddr chan string

	swarm      []*peer
	swarmTally swarmTally
	readChan   chan peerDouble

	metainfo *metainfo.Metainfo

	store      store.Store
	blocks     *bitfield.Bitfield
	requesting *bitfield.Bitfield
}

func NewSession(m *metainfo.Metainfo, config *Config) (*Session, error) {
	s := &Session{
		config:           config,
		peerId:           config.PeerId,
		infoHash:         config.InfoHash,
		name:             config.Name,
		state:            STATE_STOPPED,
		incomingPeer:     make(chan *peer, 100),
		incomingPeerAddr: make(chan string, 100),
		readChan:         make(chan peerDouble, 50),
	}

	if m != nil {
		if err := s.SetMetainfo(m); err != nil {
			return nil, err
		}
	}

	return s, nil
}

func (s *Session) SetMetainfo(m *metainfo.Metainfo) error {
	s.metainfo = m

	if s.infoHash == ZERO_HASH {
		s.infoHash = m.InfoHash
	}

	if store, err := s.config.StoreFactory.Constructor(m, s.config.StoreFactory.Config); err != nil {
		return err
	} else {
		s.store = store
	}

	if blocks, err := store.Validate(s.store); err != nil {
		return err
	} else {
		s.blocks = blocks
	}

	s.requesting = bitfield.NewBitfield(nil, s.blocks.Length())

	if s.state == STATE_LEARNING {
		logger.Debug("Got metainfo for session %x, moving to leeching state", s.infoHash)

		s.state = STATE_LEECHING
	}

	return nil
}

func (s *Session) Start() {
	logger.Info("Session starting: %x", s.infoHash)

	s.stateLock.Lock()
	if s.metainfo == nil {
		s.state = STATE_LEARNING
	} else if s.blocks.SumTrue() < s.blocks.Length() {
		s.state = STATE_LEECHING
	} else {
		s.state = STATE_SEEDING
	}
	s.stateLock.Unlock()

	for _, tkr := range s.metainfo.AnnounceList {
		tkr, err := tracker.NewTracker(tkr, s, s.incomingPeerAddr)
		if err != nil {
			logger.Error(err.Error())
			continue
		}

		s.trackers = append(s.trackers, tkr)

		tkr.Start()
	}

	// Tracker loop
	go func() {
		for {
			peerAddr := <-s.incomingPeerAddr

			// Only attempt to connect to other peers whilst leeching
			if s.state == STATE_STOPPED || s.state == STATE_SEEDING {
				continue
			}

			go func() {
				conn, err := net.Dial("tcp", peerAddr)

				if err != nil {
					logger.Debug("Failed to connect to tracker peer address %s: %s", peerAddr, err)

					return
				}

				s.AddPeer(conn, nil)
			}()
		}
	}()

	// Peer loop
	go func() {
		for {
			select {
			case peer := <-s.incomingPeer:
				logger.Debug("Connected to new peer: %s", peer.peerId)
				s.swarm = append(s.swarm, peer)
			case <-time.After(time.Second * 5):
				// Unchoke interested peers
				// TODO: Implement maximum unchoked peers
				// TODO: Implement optimistic unchoking algorithm
				for _, peer := range s.swarm {
					if peer.GetPeerInterested() && peer.GetAmChoking() {
						logger.Debug("Unchoking peer %s", peer.peerId)

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
			peerDouble := <-s.readChan
			peer := peerDouble.peer
			msg := peerDouble.msg

			switch msg := msg.(type) {
			case *keepaliveMessage:
				logger.Debug("Peer %s sent a keepalive message", peer.peerId)
			case *chokeMessage:
				logger.Debug("Peer %s has choked us", peer.peerId)
				peer.SetPeerChoking(true)
			case *unchokeMessage:
				logger.Debug("Peer %s has unchoked us", peer.peerId)
				peer.SetPeerChoking(false)
				s.MaybeQueuePieceRequests(peer)
			case *interestedMessage:
				logger.Debug("Peer %s has said it is interested", peer.peerId)
				peer.SetPeerInterested(true)
			case *uninterestedMessage:
				logger.Debug("Peer %s has said it is uninterested", peer.peerId)
				peer.SetPeerInterested(false)
			case *haveMessage:
				pieceIndex := int(msg.pieceIndex)
				logger.Debug("Peer %s has piece %d", peer.peerId, pieceIndex)
				if pieceIndex >= s.metainfo.PieceCount {
					logger.Debug("Peer %s sent an out of range have message")
					// TODO: Shutdown client
					break
				}
				peer.HasPiece(pieceIndex)
				s.swarmTally.AddIndex(pieceIndex)
				s.MaybeQueuePieceRequests(peer)
			case *bitfieldMessage:
				logger.Debug("Peer %s has sent us its bitfield", peer.peerId)
				peer.SetBitfield(msg.blocks)
				s.swarmTally.AddBitfield(msg.blocks)
				s.MaybeQueuePieceRequests(peer)
			case *requestMessage:
				if peer.GetAmChoking() || !s.blocks.Get(int(msg.pieceIndex)) || msg.blockLength > 32768 {
					logger.Debug("Peer %s has asked for a block (%d, %d, %d), but we are rejecting them", peer.peerId, msg.pieceIndex, msg.blockOffset, msg.blockLength)
					// TODO: Add naughty points
					break
				}

				logger.Debug("Peer %s has asked for a block (%d, %d, %d), going to fetch block", peer.peerId, msg.pieceIndex, msg.blockOffset, msg.blockLength)

				block := make([]byte, msg.blockLength)
				if _, err := s.store.GetBlock(int(msg.pieceIndex), int64(msg.blockOffset), block); err != nil {
					logger.Error(err.Error())
					break
				}

				logger.Debug("Peer %s has asked for a block (%d, %d, %d), sending it to them", peer.peerId, msg.pieceIndex, msg.blockOffset, msg.blockLength)

				peer.write <- &pieceMessage{
					pieceIndex:  msg.pieceIndex,
					blockOffset: msg.blockOffset,
					data:        block,
				}
			case *pieceMessage:
				rm := requestMessage{
					pieceIndex:  msg.pieceIndex,
					blockOffset: msg.blockOffset,
					blockLength: uint32(len(msg.data)),
				}

				if peer.requestsRunning.Delete(rm) == false {
					logger.Debug("Peer %s sent us a piece (%d, %d - %d) that we don't have a corresponding request for", peer.peerId, msg.pieceIndex, msg.blockOffset, int(msg.blockOffset)+len(msg.data))
				} else {
					// logger.Debug("Peer %s sent us a piece (%d, %d - %d)", peer.peerId, msg.pieceIndex, msg.blockOffset, int(msg.blockOffset)+len(msg.data))

					if _, err := s.store.SetBlock(int(msg.pieceIndex), int64(msg.blockOffset), msg.data); err != nil {
						logger.Error(err.Error())
					}
				}

				if peer.requestQueue.Len()+peer.requestsRunning.Len() == 0 {
					if ok, err := store.ValidateBlock(s.store, peer.requestingBlock); err == nil {
						if ok {
							logger.Debug("Block %d completed, marking as done", peer.requestingBlock)

							s.blocks.Set(peer.requestingBlock, true)
							s.requesting.Set(peer.requestingBlock, false)

							hm := &haveMessage{pieceIndex: uint32(peer.requestingBlock)}
							for _, peer := range s.swarm {
								peer.write <- hm
							}
						} else {
							logger.Debug("Block %d failed hash check, retrying", peer.requestingBlock)

							s.requesting.Set(peer.requestingBlock, false)
						}
					}

					peer.requestingBlock = -1

					s.MaybeQueuePieceRequests(peer)
				} else {
					peer.MaybeSendPieceRequests()
				}
			// case *cancelMessage:
			default:
				logger.Debug("Peer %s sent unknown message", peer.peerId)
			}
		}
	}()
}

func (s *Session) Name() string {
	if s.metainfo != nil {
		return s.metainfo.Name
	} else if s.name != "" {
		return s.name
	} else {
		return fmt.Sprintf("%x", s.infoHash)
	}
}

func (s *Session) InfoHash() [20]byte {
	return s.infoHash
}

func (s *Session) State() int {
	s.stateLock.Lock()
	state := s.state
	s.stateLock.Unlock()

	return state
}

func (s *Session) AddPeer(conn net.Conn, hs *handshake) {
	// Set 60 second limit to connection attempt
	conn.SetDeadline(time.Now().Add(time.Minute))

	outgoingHandshake := newHandshake(s.InfoHash(), s.PeerId())
	outgoingHandshake.flags.Set(20, true)
	logger.Debug("handshake %#v", outgoingHandshake)

	// Send handshake
	if err := outgoingHandshake.BinaryDump(conn); err != nil {
		logger.Debug("%s Failed to send handshake to connection: %s", conn.RemoteAddr(), err)
		return
	}

	// If hs is nil, this means we've attempted to establish the connection and need to wait
	// for their handshake in response
	var err error
	if hs == nil {
		if hs, err = parseHandshake(conn); err != nil {
			logger.Debug("%s failed to parse incoming handshake: %s", conn.RemoteAddr(), err)
			return
		} else if hs.infoHash != s.InfoHash() {
			logger.Debug("%s info_hash did not match for connection", conn.RemoteAddr())
			return
		}
	}

	peer := newPeer(hs, conn, s.readChan)
	peer.write <- &bitfieldMessage{blocks: s.blocks}
	s.incomingPeer <- peer

	conn.SetDeadline(time.Time{})
}

func (s *Session) Downloaded() int64 {
	// TODO:
	return 0
}

func (s *Session) Uploaded() int64 {
	// TODO:
	return 0
}

func (s *Session) Length() int64 {
	var r int64

	for _, f := range s.metainfo.Files {
		r += f.Length
	}

	return r
}

func (s *Session) Completed() int64 {
	var r int64

	for i := 0; i < s.blocks.Length(); i++ {
		if s.blocks.Get(i) {
			if i == s.blocks.Length()-1 {
				r += s.Length() % s.metainfo.PieceLength
			} else {
				r += s.metainfo.PieceLength
			}

		}
	}

	return r
}

func (s *Session) Left() int64 {
	return s.Length() - s.Completed()
}

func (s *Session) Port() uint16 {
	return s.config.Port
}

func (s *Session) PeerId() [20]byte {
	return s.peerId
}

func (s *Session) MaybeQueuePieceRequests(p *peer) {
	if p.blocks == nil {
		return
	}

	for i := 0; i < s.blocks.Length(); i++ {
		if s.blocks.Get(i) || s.requesting.Get(i) || !p.blocks.Get(i) {
			continue
		}

		if p.SetAmInterested(true) {
			p.write <- &interestedMessage{}
		}

		if p.GetPeerChoking() {
			break
		}

		if p.requestingBlock != -1 {
			break
		}

		s.requesting.Set(i, true)
		p.requestingBlock = i

		for o := 0; o < int(s.metainfo.PieceLength/8192); o++ {
			p.requestQueue.Enqueue(&requestMessage{
				pieceIndex:  uint32(i),
				blockOffset: uint32(o * 8192),
				blockLength: 8192,
			})
		}

		p.MaybeSendPieceRequests()
	}
}
