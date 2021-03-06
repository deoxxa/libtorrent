package libtorrent

import (
	"bytes"
	"fmt"
	"math"
	"net"
	"net/url"
	"sync"
	"time"

	"github.com/facebookgo/stackerr"
)

type SessionState byte

const (
	STATE_STOPPED = SessionState(iota)
	STATE_LEARNING
	STATE_LEECHING
	STATE_SEEDING
)

type Session struct {
	errors   chan error
	messages chan string

	config *Config

	infoHash [20]byte
	peerId   [20]byte
	name     string

	state     SessionState
	stateLock sync.Mutex

	peerSources []PeerSource
	trackers    []*Tracker

	connecting map[string]bool
	swarm      map[string]*Peer
	swarmTally swarmTally

	metainfo *Metainfo

	store      Store
	blocks     *Bitfield
	requesting *Bitfield
}

type peerDouble struct {
	peer *Peer
	msg  interface{}
}

func NewSession(config *Config) (*Session, error) {
	s := &Session{
		errors:   make(chan error, 100),
		messages: make(chan string, 100),

		config: config,

		peerId:   config.PeerId,
		infoHash: config.InfoHash,
		name:     config.Name,

		state: STATE_STOPPED,

		connecting: map[string]bool{},
		swarm:      map[string]*Peer{},
	}

	for _, trackerUrl := range config.Trackers {
		if err := s.AddTracker(trackerUrl); err != nil {
			return nil, stackerr.Wrap(err)
		}
	}

	for _, peerSourceFactory := range config.PeerSources {
		if peerSource, err := peerSourceFactory.Constructor(s, peerSourceFactory.Config); err != nil {
			return nil, stackerr.Wrap(err)
		} else {
			if err := s.AddPeerSource(peerSource); err != nil {
				return nil, stackerr.Wrap(err)
			}
		}
	}

	if config.Metainfo != nil {
		if err := s.SetMetainfo(config.Metainfo); err != nil {
			return nil, stackerr.Wrap(err)
		}
	}

	return s, nil
}

func (s *Session) Errors() chan error {
	return s.errors
}

func (s *Session) Messages() chan string {
	return s.messages
}

func (s *Session) InfoHash() [20]byte {
	return s.infoHash
}

func (s *Session) PeerId() [20]byte {
	return s.peerId
}

func (s *Session) IP() [4]byte {
	return s.config.IP
}

func (s *Session) Port() uint16 {
	return s.config.Port
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

func (s *Session) Length() uint64 {
	var r uint64

	if s.metainfo != nil {
		for _, f := range s.metainfo.Files {
			r += f.Length
		}
	}

	return r
}

func (s *Session) Uploaded() uint64 {
	// TODO:
	return 0
}

func (s *Session) Downloaded() uint64 {
	// TODO:
	return 0
}

func (s *Session) Completed() uint64 {
	var r uint64

	if s.blocks != nil {
		for i := 0; i < s.blocks.Length(); i++ {
			if s.blocks.Get(i) {
				if i == s.blocks.Length()-1 {
					r += s.Length() % s.metainfo.PieceLength
				} else {
					r += s.metainfo.PieceLength
				}

			}
		}
	}

	return r
}

func (s *Session) Left() uint64 {
	return s.Length() - s.Completed()
}

func (s *Session) SetMetainfo(metainfo *Metainfo) error {
	s.metainfo = metainfo

	if s.infoHash == [20]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0} {
		s.infoHash = metainfo.InfoHash
	}

	if store, err := s.config.StoreFactory.Constructor(metainfo, s.config.StoreFactory.Config); err != nil {
		return stackerr.Wrap(err)
	} else {
		s.store = store
	}

	if blocks, err := validate(s.store); err != nil {
		return stackerr.Wrap(err)
	} else {
		s.blocks = blocks
	}

	s.requesting = NewBitfield(nil, s.blocks.Length())

	if s.state == STATE_LEARNING {
		s.stateLock.Lock()
		s.state = STATE_LEECHING
		s.stateLock.Unlock()
	}

	for _, peerSource := range s.peerSources {
		if err := peerSource.Metainfo(metainfo); err != nil {
			s.errors <- stackerr.Wrap(err)
		}
	}

	for i := 0; i < s.blocks.Length(); i++ {
		if s.blocks.Get(i) {
			for _, peer := range s.swarm {
				peer.Outgoing <- &haveMessage{pieceIndex: uint32(i)}
			}
		}
	}

	for _, peer := range s.swarm {
		peer.maybeCancelMetadataRequests()
		s.maybeQueuePieceRequests(peer)
	}

	return nil
}

func (s *Session) AddTracker(trackerUrl string) error {
	u, err := url.Parse(trackerUrl)
	if err != nil {
		return stackerr.Wrap(err)
	}

	transportFactory, ok := s.config.TrackerTransports[u.Scheme]
	if !ok {
		return stackerr.Newf("unrecognised tracker scheme %s for %s", u.Scheme, trackerUrl)
	}

	transport, err := transportFactory.Constructor(u, transportFactory.Config)
	if err != nil {
		return stackerr.Wrap(err)
	}

	tracker, err := NewTracker(transport, s)
	if err != nil {
		return stackerr.Wrap(err)
	}

	if err := s.AddPeerSource(tracker); err != nil {
		return stackerr.Wrap(err)
	}

	s.trackers = append(s.trackers, tracker)

	return nil
}

func (s *Session) AddPeerSource(peerSource PeerSource) error {
	if s.metainfo != nil {
		if err := peerSource.Metainfo(s.metainfo); err != nil {
			return stackerr.Wrap(err)
		}
	}

	if s.state != STATE_STOPPED {
		if err := peerSource.Start(); err != nil {
			return stackerr.Wrap(err)
		}
	}

	s.peerSources = append(s.peerSources, peerSource)

	go func() {
		for addr := range peerSource.Peers() {
			s.AddPeerAddress(addr)
		}
	}()

	go func() {
		for err := range peerSource.Errors() {
			s.errors <- stackerr.Wrap(err)
		}
	}()

	return nil
}

func (s *Session) Start() error {
	s.stateLock.Lock()

	if s.metainfo == nil {
		s.state = STATE_LEARNING
	} else if s.blocks.Sum() < s.blocks.Length() {
		s.state = STATE_LEECHING
	} else {
		s.state = STATE_SEEDING
	}

	for _, peerSource := range s.peerSources {
		if err := peerSource.Start(); err != nil {
			s.errors <- stackerr.Wrap(err)
		}
	}

	s.stateLock.Unlock()

	return nil
}

func (s *Session) Stop() error {
	s.stateLock.Lock()
	s.state = STATE_STOPPED
	s.stateLock.Unlock()

	for _, peerSource := range s.peerSources {
		if err := peerSource.Stop(); err != nil {
			s.errors <- stackerr.Wrap(err)
		}
	}

	return nil
}

func (s *Session) State() SessionState {
	s.stateLock.Lock()
	state := s.state
	s.stateLock.Unlock()

	return state
}

func (s *Session) AddPeerAddress(peerAddress *PeerAddress) {
	if s.state == STATE_STOPPED || s.state == STATE_SEEDING {
		return
	}

	go s.ConnectToPeer(peerAddress)
}

func (s *Session) AddPeer(conn net.Conn, hs *handshake) error {
	// Set 60 second limit to handshake attempt
	conn.SetDeadline(time.Now().Add(time.Minute))

	outgoingHandshake := newHandshake(s.InfoHash(), s.PeerId())
	outgoingHandshake.flags.Set(43, true)

	if err := outgoingHandshake.BinaryDump(conn); err != nil {
		return stackerr.Wrap(err)
	}

	// If hs is nil, this means we've attempted to establish the connection and need to wait
	// for their handshake in response
	var err error
	if hs == nil {
		if hs, err = parseHandshake(conn); err != nil {
			return stackerr.Wrap(err)
		} else if hs.infoHash != s.InfoHash() {
			return stackerr.New("info_hash didn't match")
		}
	}

	peer := NewPeer(conn.RemoteAddr().String(), hs, conn)

	if s.blocks != nil {
		peer.Outgoing <- &bitfieldMessage{blocks: s.blocks}
	}

	conn.SetDeadline(time.Time{})

	s.swarm[peer.Addr()] = peer

	go s.readErrorsFromPeer(peer)
	go s.readMessagesFromPeer(peer)

	return nil
}

func (s *Session) RemovePeer(peer *Peer) error {
	if peer.requestingBlock != -1 {
		s.requesting.Set(peer.requestingBlock, false)
	}

	delete(s.swarm, peer.Addr())

	return nil
}

func (s *Session) ConnectToPeer(peerAddress *PeerAddress) error {
	p := fmt.Sprintf("%s:%d", peerAddress.Host, peerAddress.Port)

	if _, ok := s.swarm[p]; ok {
		return stackerr.New("already connected to this peer")
	}

	if _, ok := s.connecting[p]; ok {
		return stackerr.New("already connecting to this peer")
	}

	s.connecting[p] = true

	defer delete(s.connecting, p)

	if conn, err := net.Dial("tcp", p); err != nil {
		return stackerr.Wrap(err)
	} else if err := s.AddPeer(conn, nil); err != nil {
		return stackerr.Wrap(err)
	}

	return nil
}

func (s *Session) readErrorsFromPeer(peer *Peer) {
	for err := range peer.Errors {
		s.errors <- stackerr.Wrap(err)
	}
}

func (s *Session) readMessagesFromPeer(peer *Peer) {
	for msg := range peer.Incoming {
		switch msg := msg.(type) {
		case *keepaliveMessage:
		case *chokeMessage:
			peer.SetPeerChoking(true)
		case *unchokeMessage:
			peer.SetPeerChoking(false)

			if s.metainfo != nil {
				s.maybeQueuePieceRequests(peer)
			}
		case *interestedMessage:
			peer.SetPeerInterested(true)

			if peer.SetAmChoking(false) {
				peer.Outgoing <- &unchokeMessage{}
			}
		case *uninterestedMessage:
			peer.SetPeerInterested(false)
		case *haveMessage:
			pieceIndex := int(msg.pieceIndex)

			peer.MarkPieceComplete(pieceIndex)

			if s.metainfo != nil {
				if pieceIndex >= s.metainfo.PieceCount {
					break
				}

				s.swarmTally.AddIndex(pieceIndex)

				if s.blocks != nil && !s.blocks.Get(pieceIndex) {
					s.maybeQueuePieceRequests(peer)
				}
			}
		case *bitfieldMessage:
			peer.SetBitfield(msg.blocks)

			if s.metainfo != nil {
				s.swarmTally.AddBitfield(msg.blocks)

				s.maybeQueuePieceRequests(peer)
			}
		case *requestMessage:
			if s.metainfo != nil {
				break
			}

			if peer.GetAmChoking() || !s.blocks.Get(int(msg.pieceIndex)) || msg.blockLength > 32768 {
				break
			}

			block := make([]byte, msg.blockLength)
			if _, err := s.store.GetBlock(int(msg.pieceIndex), int64(msg.blockOffset), block); err != nil {
				s.errors <- stackerr.Wrap(err)
				break
			}

			peer.Outgoing <- &pieceMessage{
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

			if peer.requestsRunning.Delete(rm) {
				if _, err := s.store.SetBlock(int(msg.pieceIndex), int64(msg.blockOffset), msg.data); err != nil {
					s.errors <- stackerr.Wrap(err)
				}
			} else {
				s.errors <- stackerr.New("peer sent us a piece that we didn't have a request for")

				break
			}

			if peer.requestQueue.Len()+peer.requestsRunning.Len() == 0 {
				if ok, err := validateBlock(s.store, peer.requestingBlock); err != nil {
					s.errors <- stackerr.Wrap(err)
				} else {
					if ok {
						s.blocks.Set(peer.requestingBlock, true)
						s.requesting.Set(peer.requestingBlock, false)

						hm := &haveMessage{pieceIndex: uint32(peer.requestingBlock)}
						for _, peer := range s.swarm {
							peer.Outgoing <- hm
						}
					} else {
						s.requesting.Set(peer.requestingBlock, false)
					}
				}

				peer.requestingBlock = -1

				if s.blocks.Sum() == s.blocks.Length() {
					s.stateLock.Lock()
					s.state = STATE_SEEDING
					s.stateLock.Unlock()
				}

				if s.metainfo != nil {
					s.maybeQueuePieceRequests(peer)
				}
			} else {
				peer.maybeSendPieceRequests()
			}
		// case *cancelMessage:
		case *extendedHandshakeMessage:
			for name, id := range msg.Messages {
				peer.extensionIds[name] = id
				peer.extensionNames[id] = name
			}

			if msg.Version != "" {
				peer.version = msg.Version
			}

			if msg.YourIP != "" {
				peer.reportedIp = msg.YourIP
			}

			if !bytes.Equal(msg.IPV4, []byte{0, 0, 0, 0}) {
				copy(peer.reportedIpv4[:], msg.IPV4)
			}

			if !bytes.Equal(msg.IPV6, []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}) {
				copy(peer.reportedIpv6[:], msg.IPV6)
			}

			if msg.RequestQueue != 0 {
				peer.maxRequests = msg.RequestQueue
			}

			peer.Outgoing <- &extendedHandshakeMessage{
				Messages: msg.Messages,
			}

			if _, ok := peer.extensionIds["ut_metadata"]; ok && msg.MetadataSize != 0 && s.metainfo == nil {
				t := int(math.Ceil(float64(msg.MetadataSize) / 16384))

				peer.metadataPieces = NewBitfield(nil, t)
				peer.metadataContent = make([]byte, msg.MetadataSize)

				for i := 0; i < t; i++ {
					peer.metadataQueue.Enqueue(&extendedUtMetadataMessage{
						Type:  0,
						Piece: i,
					})
				}

				peer.maybeSendMetadataRequests()
			}
		case *extendedUtMetadataMessage:
			if msg.Type == 1 && peer.metadataContent != nil && s.metainfo == nil {
				copy(peer.metadataContent[msg.Piece*16384:], msg.Data)

				peer.metadataPieces.Set(msg.Piece, true)

				if peer.metadataPieces.Sum() == peer.metadataPieces.Length() && s.metainfo == nil {
					if metainfo, err := ParseInfoDict(peer.metadataContent); err != nil {
						s.errors <- stackerr.Wrap(err)
					} else if metainfo.InfoHash != s.InfoHash() {
						s.errors <- stackerr.New("metadata received from peer didn't match info_hash")
					} else {
						s.SetMetainfo(metainfo)
					}

					peer.maybeCancelMetadataRequests()
				} else {
					peer.maybeSendMetadataRequests()
				}
			}
		case *extendedUploadOnlyMessage:
			peer.uploadOnly = msg.UploadOnly
		case *extendedUtPeerExchangeMessage:
			for _, peerAddress := range msg.Added {
				s.AddPeerAddress(&peerAddress)
			}
		case *extendedLtTrackerExchangeMessage:
			for _, trackerUrl := range msg.Added {
				s.AddTracker(trackerUrl)
			}
		}
	}

	if err := s.RemovePeer(peer); err != nil {
		s.errors <- stackerr.Wrap(err)
	}
}

func (s *Session) maybeQueuePieceRequests(peer *Peer) {
	if peer.blocks == nil {
		return
	}

	if s.blocks == nil {
		return
	}

	for i := 0; i < s.blocks.Length(); i++ {
		if s.blocks.Get(i) {
			continue
		}

		// we don't have the full bitfield from this peer yet
		if peer.blocks.Length() <= i {
			continue
		}

		if s.requesting.Get(i) {
			continue
		}

		if !peer.blocks.Get(i) {
			continue
		}

		if peer.SetAmInterested(true) {
			peer.Outgoing <- &interestedMessage{}
		}

		if peer.GetPeerChoking() {
			break
		}

		if peer.requestingBlock != -1 {
			break
		}

		s.requesting.Set(i, true)
		peer.requestingBlock = i

		for o := 0; o < int(s.metainfo.PieceLength/8192); o++ {
			peer.requestQueue.Enqueue(&requestMessage{
				pieceIndex:  uint32(i),
				blockOffset: uint32(o * 8192),
				blockLength: 8192,
			})
		}

		peer.maybeSendPieceRequests()
	}
}
