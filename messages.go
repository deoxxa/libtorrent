package libtorrent

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"io/ioutil"
	"math"

	"github.com/facebookgo/stackerr"
	"github.com/torrance/libtorrent/bitfield"
)

const (
	Choke = uint8(iota)
	Unchoke
	Interested
	Uninterested
	Have
	Bitfield
	Request
	Piece
	Cancel
)

type binaryDumper interface {
	BinaryDump(w io.Writer) error
}

type handshake struct {
	protocol []byte
	flags    *bitfield.Bitfield
	infoHash [20]byte
	peerId   [20]byte
}

func newHandshake(infoHash [20]byte, peerId [20]byte) (hs *handshake) {
	return &handshake{
		protocol: []byte("BitTorrent protocol"),
		flags:    bitfield.NewBitfield(nil, 64),
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
	hs.flags = bitfield.NewBitfield(reserved, 64)

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

func parsePeerMessage(r io.Reader) (interface{}, error) {
	// Read message length (4 bytes)
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return nil, stackerr.Wrap(err)
	} else if length == 0 {
		// Keepalive message
		return parseKeepaliveMessage(r)
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

	switch id {
	case Choke:
		return parseChokeMessage(payloadReader)
	case Unchoke:
		return parseUnchokeMessage(payloadReader)
	case Interested:
		return parseInterestedMessage(payloadReader)
	case Uninterested:
		return parseUninterestedMessage(payloadReader)
	case Have:
		return parseHaveMessage(payloadReader)
	case Bitfield:
		return parseBitfieldMessage(payloadReader)
	case Request:
		return parseRequestMessage(payloadReader)
	case Piece:
		return parsePieceMessage(payloadReader)
	}

	return nil, stackerr.Wrap(unknownMessage{id: id, length: length})
}

type keepaliveMessage struct{}

func parseKeepaliveMessage(r io.Reader) (*keepaliveMessage, error) {
	return new(keepaliveMessage), nil
}

func (msg *keepaliveMessage) BinaryDump(w io.Writer) error {
	mw := monadWriter{w: w}
	mw.Write(uint32(0))

	if mw.err == nil {
		return nil
	} else {
		return stackerr.Wrap(mw.err)
	}
}

type chokeMessage struct{}

func parseChokeMessage(r io.Reader) (*chokeMessage, error) {
	return new(chokeMessage), nil
}

func (msg *chokeMessage) BinaryDump(w io.Writer) error {
	mw := monadWriter{w: w}
	mw.Write(uint32(1))
	mw.Write(Choke)

	if mw.err == nil {
		return nil
	} else {
		return stackerr.Wrap(mw.err)
	}
}

type unchokeMessage struct{}

func parseUnchokeMessage(r io.Reader) (*unchokeMessage, error) {
	return new(unchokeMessage), nil
}

func (msg *unchokeMessage) BinaryDump(w io.Writer) error {
	mw := monadWriter{w: w}
	mw.Write(uint32(1))
	mw.Write(Unchoke)

	if mw.err == nil {
		return nil
	} else {
		return stackerr.Wrap(mw.err)
	}
}

type interestedMessage struct{}

func parseInterestedMessage(r io.Reader) (*interestedMessage, error) {
	return new(interestedMessage), nil
}

func (msg *interestedMessage) BinaryDump(w io.Writer) error {
	mw := monadWriter{w: w}
	mw.Write(uint32(1))
	mw.Write(Interested)
	if mw.err == nil {
		return nil
	} else {
		return stackerr.Wrap(mw.err)
	}
}

type uninterestedMessage struct{}

func parseUninterestedMessage(r io.Reader) (*uninterestedMessage, error) {
	return new(uninterestedMessage), nil
}

func (msg *uninterestedMessage) BinaryDump(w io.Writer) error {
	mw := monadWriter{w: w}
	mw.Write(uint32(1))
	mw.Write(Uninterested)

	if mw.err == nil {
		return nil
	} else {
		return stackerr.Wrap(mw.err)
	}
}

type haveMessage struct {
	pieceIndex uint32
}

func parseHaveMessage(r io.Reader) (*haveMessage, error) {
	msg := new(haveMessage)
	mr := monadReader{r: r}
	mr.Read(&msg.pieceIndex)

	if mr.err == nil {
		return msg, nil
	} else {
		return nil, stackerr.Wrap(mr.err)
	}
}

func (msg *haveMessage) BinaryDump(w io.Writer) error {
	mw := monadWriter{w: w}
	mw.Write(uint32(5))
	mw.Write(Have)
	mw.Write(msg.pieceIndex)
	if mw.err == nil {
		return nil
	} else {
		return stackerr.Wrap(mw.err)
	}
}

type bitfieldMessage struct {
	blocks *bitfield.Bitfield
}

func parseBitfieldMessage(r io.Reader) (msg *bitfieldMessage, err error) {
	if data, err := ioutil.ReadAll(r); err != nil {
		return nil, stackerr.Wrap(err)
	} else {
		msg = &bitfieldMessage{
			blocks: bitfield.NewBitfield(data, len(data)*8),
		}

		return msg, nil
	}
}

func (msg *bitfieldMessage) BinaryDump(w io.Writer) error {
	mw := monadWriter{w: w}
	mw.Write(uint32(math.Ceil(float64(msg.blocks.Length())/8) + 1))
	mw.Write(Bitfield)
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

func parseRequestMessage(r io.Reader) (msg *requestMessage, err error) {
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

func (msg requestMessage) BinaryDump(w io.Writer) (err error) {
	mw := &monadWriter{w: w}
	mw.Write(uint32(13)) // Length: status + 12 byte payload
	mw.Write(Request)    // Message id
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

func parsePieceMessage(r io.Reader) (msg *pieceMessage, err error) {
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

func (msg *pieceMessage) BinaryDump(w io.Writer) error {
	length := uint32(len(msg.data) + 9)
	mw := monadWriter{w: w}
	mw.Write(length)
	mw.Write(Piece)
	mw.Write(msg.pieceIndex)
	mw.Write(msg.blockOffset)
	mw.Write(msg.data)
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
