package libtorrent

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"io/ioutil"
	"math"

	"github.com/facebookgo/stackerr"
	"github.com/zeebo/bencode"
)

type MessageType byte

const (
	MESSAGE_CHOKE = MessageType(iota)
	MESSAGE_UNCHOKE
	MESSAGE_INTERESTED
	MESSAGE_UNINTERESTED
	MESSAGE_HAVE
	MESSAGE_BITFIELD
	MESSAGE_REQUEST
	MESSAGE_PIECE
	MESSAGE_CANCEL
	MESSAGE_EXTENDED = MessageType(20)
)

type binaryDumper interface {
	BinaryDump(p *Peer, w io.Writer) error
}

type handshake struct {
	protocol []byte
	flags    *Bitfield
	infoHash [20]byte
	peerId   [20]byte
}

func newHandshake(infoHash [20]byte, peerId [20]byte) (hs *handshake) {
	return &handshake{
		protocol: []byte("BitTorrent protocol"),
		flags:    NewBitfield(nil, 64),
		infoHash: infoHash,
		peerId:   peerId,
	}
}

func parseHandshake(r io.Reader) (*handshake, error) {
	buf := make([]byte, 20)
	hs := new(handshake)

	// Name length
	if _, err := r.Read(buf[0:1]); err != nil {
		return nil, stackerr.Wrap(err)
	} else if int(buf[0]) != 19 {
		return nil, stackerr.New("Handshake halted: name length was not 19")
	}

	// Protocol
	if _, err := r.Read(buf[0:19]); err != nil {
		return nil, stackerr.Wrap(err)
	} else if !bytes.Equal(buf[0:19], []byte("BitTorrent protocol")) {
		return nil, stackerr.Newf("Handshake halted: incompatible protocol: %s", buf[0:19])
	}
	hs.protocol = append(hs.protocol, buf[0:19]...)

	// Reserved bits
	reserved := make([]byte, 8)
	if _, err := r.Read(reserved); err != nil {
		return nil, stackerr.Wrap(err)
	}
	hs.flags = NewBitfield(reserved, 64)

	// Info Hash
	if _, err := r.Read(hs.infoHash[:]); err != nil {
		return nil, stackerr.Wrap(err)
	}

	// PeerID
	if _, err := r.Read(hs.peerId[:]); err != nil {
		return nil, stackerr.Wrap(err)
	}

	return hs, nil
}

func (hs *handshake) BinaryDump(w io.Writer) error {
	mw := &monadWriter{w: w}
	mw.Write(uint8(19))        // Name length
	mw.Write(hs.protocol)      // Protocol name
	mw.Write(hs.flags.Bytes()) // Reserved 8 bytes
	mw.Write(hs.infoHash)      // InfoHash
	mw.Write(hs.peerId)        // PeerId

	if mw.err == nil {
		return nil
	} else {
		return stackerr.Wrap(mw.err)
	}
}

func (hs *handshake) String() string {
	return fmt.Sprintf("[Handshake Protocol: %s infoHash: %x peerId: %s]", hs.protocol, hs.infoHash, hs.peerId)
}

func parsePeerMessage(p *Peer, r io.Reader) (interface{}, error) {
	// Read message length (4 bytes)
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return nil, stackerr.Wrap(err)
	} else if length == 0 {
		// Keepalive message
		return parseKeepaliveMessage(p, r)
	} else if length > 131072 {
		// Set limit at 2^17. Might need to revise this later
		return nil, stackerr.Newf("Message size too long: %d", length)
	}

	// Read message id (1 byte)
	var id uint8
	if err := binary.Read(r, binary.BigEndian, &id); err != nil {
		return nil, stackerr.Wrap(err)
	}

	// Read payload (arbitrary size)
	payload := make([]byte, length-1)
	if length-1 > 0 {
		if err := binary.Read(r, binary.BigEndian, payload); err != nil {
			return nil, stackerr.Wrap(err)
		}
	}
	payloadReader := bytes.NewReader(payload)

	switch MessageType(id) {
	case MESSAGE_CHOKE:
		return parseChokeMessage(p, payloadReader)
	case MESSAGE_UNCHOKE:
		return parseUnchokeMessage(p, payloadReader)
	case MESSAGE_INTERESTED:
		return parseInterestedMessage(p, payloadReader)
	case MESSAGE_UNINTERESTED:
		return parseUninterestedMessage(p, payloadReader)
	case MESSAGE_HAVE:
		return parseHaveMessage(p, payloadReader)
	case MESSAGE_BITFIELD:
		return parseBitfieldMessage(p, payloadReader)
	case MESSAGE_REQUEST:
		return parseRequestMessage(p, payloadReader)
	case MESSAGE_PIECE:
		return parsePieceMessage(p, payloadReader)
	case MESSAGE_EXTENDED:
		return parseExtendedMessage(p, payloadReader)
	}

	return nil, stackerr.Wrap(unknownMessage{id: id, length: length})
}

type keepaliveMessage struct{}

func parseKeepaliveMessage(p *Peer, r io.Reader) (*keepaliveMessage, error) {
	return new(keepaliveMessage), nil
}

func (msg *keepaliveMessage) BinaryDump(p *Peer, w io.Writer) error {
	mw := monadWriter{w: w}
	mw.Write(uint32(0))

	if mw.err == nil {
		return nil
	} else {
		return stackerr.Wrap(mw.err)
	}
}

type chokeMessage struct{}

func parseChokeMessage(p *Peer, r io.Reader) (*chokeMessage, error) {
	return new(chokeMessage), nil
}

func (msg *chokeMessage) BinaryDump(p *Peer, w io.Writer) error {
	mw := monadWriter{w: w}
	mw.Write(uint32(1))
	mw.Write(MESSAGE_CHOKE)

	if mw.err == nil {
		return nil
	} else {
		return stackerr.Wrap(mw.err)
	}
}

type unchokeMessage struct{}

func parseUnchokeMessage(p *Peer, r io.Reader) (*unchokeMessage, error) {
	return new(unchokeMessage), nil
}

func (msg *unchokeMessage) BinaryDump(p *Peer, w io.Writer) error {
	mw := monadWriter{w: w}
	mw.Write(uint32(1))
	mw.Write(MESSAGE_UNCHOKE)

	if mw.err == nil {
		return nil
	} else {
		return stackerr.Wrap(mw.err)
	}
}

type interestedMessage struct{}

func parseInterestedMessage(p *Peer, r io.Reader) (*interestedMessage, error) {
	return new(interestedMessage), nil
}

func (msg *interestedMessage) BinaryDump(p *Peer, w io.Writer) error {
	mw := monadWriter{w: w}
	mw.Write(uint32(1))
	mw.Write(MESSAGE_INTERESTED)
	if mw.err == nil {
		return nil
	} else {
		return stackerr.Wrap(mw.err)
	}
}

type uninterestedMessage struct{}

func parseUninterestedMessage(p *Peer, r io.Reader) (*uninterestedMessage, error) {
	return new(uninterestedMessage), nil
}

func (msg *uninterestedMessage) BinaryDump(p *Peer, w io.Writer) error {
	mw := monadWriter{w: w}
	mw.Write(uint32(1))
	mw.Write(MESSAGE_UNINTERESTED)

	if mw.err == nil {
		return nil
	} else {
		return stackerr.Wrap(mw.err)
	}
}

type haveMessage struct {
	pieceIndex uint32
}

func parseHaveMessage(p *Peer, r io.Reader) (*haveMessage, error) {
	msg := new(haveMessage)
	mr := monadReader{r: r}
	mr.Read(&msg.pieceIndex)

	if mr.err == nil {
		return msg, nil
	} else {
		return nil, stackerr.Wrap(mr.err)
	}
}

func (msg *haveMessage) BinaryDump(p *Peer, w io.Writer) error {
	mw := monadWriter{w: w}
	mw.Write(uint32(5))
	mw.Write(MESSAGE_HAVE)
	mw.Write(msg.pieceIndex)
	if mw.err == nil {
		return nil
	} else {
		return stackerr.Wrap(mw.err)
	}
}

type bitfieldMessage struct {
	blocks *Bitfield
}

func parseBitfieldMessage(p *Peer, r io.Reader) (msg *bitfieldMessage, err error) {
	if data, err := ioutil.ReadAll(r); err != nil {
		return nil, stackerr.Wrap(err)
	} else {
		msg = &bitfieldMessage{
			blocks: NewBitfield(data, len(data)*8),
		}

		return msg, nil
	}
}

func (msg *bitfieldMessage) BinaryDump(p *Peer, w io.Writer) error {
	mw := monadWriter{w: w}
	mw.Write(uint32(math.Ceil(float64(msg.blocks.Length())/8) + 1))
	mw.Write(MESSAGE_BITFIELD)
	mw.Write(msg.blocks.Bytes())
	if mw.err == nil {
		return nil
	} else {
		return stackerr.Wrap(mw.err)
	}
}

func (msg *bitfieldMessage) String() string {
	return "Bitfield message"
}

type requestMessage struct {
	pieceIndex  uint32
	blockOffset uint32
	blockLength uint32
}

func parseRequestMessage(p *Peer, r io.Reader) (msg *requestMessage, err error) {
	msg = new(requestMessage)
	mr := &monadReader{r: r}
	mr.Read(&msg.pieceIndex)
	mr.Read(&msg.blockOffset)
	mr.Read(&msg.blockLength)
	if mr.err == nil {
		return msg, nil
	} else {
		return nil, stackerr.Wrap(mr.err)
	}
}

func (msg requestMessage) BinaryDump(p *Peer, w io.Writer) (err error) {
	mw := &monadWriter{w: w}
	mw.Write(uint32(13))      // Length: status + 12 byte payload
	mw.Write(MESSAGE_REQUEST) // Message id
	mw.Write(msg.pieceIndex)
	mw.Write(msg.blockOffset)
	mw.Write(msg.blockLength)
	if mw.err == nil {
		return nil
	} else {
		return stackerr.Wrap(mw.err)
	}
}

type pieceMessage struct {
	pieceIndex  uint32
	blockOffset uint32
	data        []byte
}

func parsePieceMessage(p *Peer, r io.Reader) (msg *pieceMessage, err error) {
	msg = new(pieceMessage)

	mr := &monadReader{r: r}
	mr.Read(&msg.pieceIndex)
	mr.Read(&msg.blockOffset)

	if mr.err != nil {
		return nil, stackerr.Wrap(mr.err)
	}

	if data, err := ioutil.ReadAll(r); err != nil {
		return nil, stackerr.Wrap(err)
	} else {
		msg.data = data
	}

	return msg, nil
}

func (msg *pieceMessage) BinaryDump(p *Peer, w io.Writer) error {
	length := uint32(len(msg.data) + 9)
	mw := monadWriter{w: w}
	mw.Write(length)
	mw.Write(MESSAGE_PIECE)
	mw.Write(msg.pieceIndex)
	mw.Write(msg.blockOffset)
	mw.Write(msg.data)
	if mw.err == nil {
		return nil
	} else {
		return stackerr.Wrap(mw.err)
	}
}

func parseExtendedMessage(p *Peer, r io.Reader) (interface{}, error) {
	mr := &monadReader{r: r}

	var messageId uint8
	mr.Read(&messageId)

	if mr.err != nil {
		return nil, stackerr.Wrap(mr.err)
	}

	if messageId == 0 {
		return parseExtendedHandshakeMessage(p, r)
	}

	messageName, ok := p.extensionNames[int(messageId)]
	if !ok {
		return nil, stackerr.Newf("unknown extended message id: %d", messageId)
	}

	switch messageName {
	case "ut_metadata":
		return parseExtendedUtMetadataMessage(p, r)
	}

	return nil, stackerr.Newf("unknown extended message type: %s (%d)", messageName, messageId)
}

type extendedMessage struct {
	Name    string
	Message binaryDumper
}

type extendedHandshakeMessage struct {
	Messages     map[string]int `bencode:"m"`
	Port         int            `bencode:"p"`
	Version      string         `bencode:"v"`
	YourIP       string         `bencode:"yourip"`
	IPV4         [4]byte        `bencode:"ipv4"`
	IPV6         [16]byte       `bencode:"ipv6"`
	RequestQueue int            `bencode:"reqq"`
	MetadataSize int            `bencode:"metadata_size"`
}

func parseExtendedHandshakeMessage(p *Peer, r io.Reader) (*extendedHandshakeMessage, error) {
	var m extendedHandshakeMessage
	d := bencode.NewDecoder(r)
	if err := d.Decode(&m); err != nil {
		return nil, stackerr.Wrap(err)
	}

	return &m, nil
}

func (msg *extendedHandshakeMessage) BinaryDump(p *Peer, w io.Writer) error {
	data, err := bencode.EncodeString(msg)
	if err != nil {
		return stackerr.Wrap(err)
	}

	length := uint32(2 + len(data))

	mw := monadWriter{w: w}
	mw.Write(length)
	mw.Write(MESSAGE_EXTENDED)
	mw.Write(uint8(0))
	mw.Write([]byte(data))

	if mw.err == nil {
		return nil
	} else {
		return stackerr.Wrap(mw.err)
	}
}

type extendedUtMetadataMessage struct {
	Type  int    `bencode:"msg_type"`
	Piece int    `bencode:"piece"`
	Data  []byte `bencode:"-"`
}

func parseExtendedUtMetadataMessage(p *Peer, r io.Reader) (*extendedUtMetadataMessage, error) {
	data, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, stackerr.Wrap(err)
	}

	var m extendedUtMetadataMessage
	d := bencode.NewDecoder(bytes.NewReader(data))
	if err := d.Decode(&m); err != nil {
		return nil, stackerr.Wrap(err)
	}

	if m.Type == 1 {
		m.Data = data[d.BytesParsed():]
	}

	return &m, nil
}

func (msg *extendedUtMetadataMessage) BinaryDump(p *Peer, w io.Writer) error {
	messageId, ok := p.extensionIds["ut_metadata"]
	if !ok {
		return stackerr.Newf("peer doesn't understand metadata messages")
	}

	data, err := bencode.EncodeString(msg)
	if err != nil {
		return stackerr.Wrap(err)
	}

	length := uint32(2 + len(data) + len(msg.Data))

	mw := monadWriter{w: w}
	mw.Write(length)
	mw.Write(MESSAGE_EXTENDED)
	mw.Write(uint8(messageId))
	mw.Write([]byte(data))
	if msg.Data != nil {
		mw.Write(msg.Data)
	}

	if mw.err == nil {
		return nil
	} else {
		return stackerr.Wrap(mw.err)
	}
}

type unknownMessage struct {
	id     uint8
	length uint32
}

func (e unknownMessage) Error() string {
	return fmt.Sprintf("Unknown message id: %d, length: %d", e.id, e.length)
}
