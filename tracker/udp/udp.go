package udp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/url"
	"time"

	"github.com/facebookgo/stackerr"
	"github.com/torrance/libtorrent"
)

type Transport struct {
	u    *url.URL
	conn net.Conn
	cid  uint64
}

func NewTransport(u *url.URL, config interface{}) (libtorrent.TrackerTransport, error) {
	conn, err := net.Dial("udp", u.Host)
	if err != nil {
		return nil, stackerr.Wrap(err)
	}

	creq := &connectionRequest{transactionId: uint32(rand.Int31())}
	if err = creq.BinaryDump(conn); err != nil {
		return nil, stackerr.Wrap(err)
	}

	cres, err := parseConnectionResponse(conn)
	if err != nil {
		return nil, stackerr.Wrap(err)
	} else if cres.transactionId != creq.transactionId {
		return nil, stackerr.New("recieved transactionId did not match")
	} else if cres.action != 0 {
		return nil, stackerr.Newf("action is not set to connect (0), instead got %d", cres.action)
	}

	t := &Transport{
		u:    u,
		conn: conn,
		cid:  cres.connectionId,
	}

	return t, nil
}

func (u *Transport) Announce(req *libtorrent.TrackerAnnounceRequest) (*libtorrent.TrackerAnnounceResponse, error) {
	areq := announceRequest{
		connectionId:  u.cid,
		transactionId: uint32(rand.Int31()),
		infoHash:      req.InfoHash,
		peerId:        req.PeerId,
		downloaded:    req.Downloaded,
		left:          req.Left,
		uploaded:      req.Uploaded,
		event:         int32(req.Event),
		ipAddress:     (uint32(req.IP[0]) << 24) + (uint32(req.IP[1]) << 16) + (uint32(req.IP[2]) << 8) + uint32(req.IP[3]),
		port:          req.Port,
		// key:           req.Key,
		// numWant:       req.NumWant,
	}

	areq.connectionId = u.cid
	if err := areq.BinaryDump(u.conn); err != nil {
		return nil, stackerr.Wrap(err)
	}

	ares, err := parseAnnounceResponse(u.conn)
	if err != nil {
		return nil, stackerr.Wrap(err)
	} else if ares.transactionId != areq.transactionId {
		return nil, stackerr.New("received transactionId did not match")
	} else if ares.action != 1 {
		return nil, stackerr.Newf("action is not set to announce (1), instead got %d", ares.action)
	}

	r := &libtorrent.TrackerAnnounceResponse{
		Leechers:          uint32(ares.leechers),
		Seeders:           uint32(ares.seeders),
		Peers:             ares.peers,
		RequiredInterval:  time.Duration(ares.interval) * time.Second,
		SuggestedInterval: time.Duration(ares.interval) * time.Second,
	}

	return r, nil
}

type connectionRequest struct {
	transactionId uint32
}

func (c *connectionRequest) BinaryDump(w io.Writer) (err error) {
	// We write out to a buffer first before writing to w, as
	// we need this to go out in a single Transport packet
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, uint64(0x41727101980))
	binary.Write(buf, binary.BigEndian, uint32(0))
	binary.Write(buf, binary.BigEndian, c.transactionId)
	_, err = w.Write(buf.Bytes())
	return
}

type connectionResponse struct {
	transactionId uint32
	connectionId  uint64
	action        uint32
}

func parseConnectionResponse(r io.Reader) (conRes *connectionResponse, err error) {
	b := make([]byte, 16)
	n, err := r.Read(b)
	if err != nil {
		return
	} else if n < 16 {
		err = stackerr.New("parseConnectionResponse: Transport packet less than 16 bytes")
		return
	}

	buf := bytes.NewReader(b)
	conRes = new(connectionResponse)
	binary.Read(buf, binary.BigEndian, &conRes.action)
	binary.Read(buf, binary.BigEndian, &conRes.transactionId)
	binary.Read(buf, binary.BigEndian, &conRes.connectionId)
	return
}

type announceRequest struct {
	connectionId  uint64
	transactionId uint32
	infoHash      [20]byte
	peerId        [20]byte
	downloaded    int64
	left          int64
	uploaded      int64
	event         int32
	ipAddress     uint32
	key           uint32
	numWant       int32
	port          uint16
}

func (a *announceRequest) BinaryDump(w io.Writer) (err error) {
	// Ensure default values are set
	if a.numWant == 0 {
		a.numWant = -1
	}

	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, a.connectionId)  // Connection id
	binary.Write(buf, binary.BigEndian, uint32(1))       // Action
	binary.Write(buf, binary.BigEndian, a.transactionId) // Transaction id
	binary.Write(buf, binary.BigEndian, a.infoHash)      // Infohash
	binary.Write(buf, binary.BigEndian, a.peerId)        // Peer id
	binary.Write(buf, binary.BigEndian, a.downloaded)    // Downloaded
	binary.Write(buf, binary.BigEndian, a.left)          // Left
	binary.Write(buf, binary.BigEndian, a.uploaded)      // Uploaded
	binary.Write(buf, binary.BigEndian, a.event)         // Event
	binary.Write(buf, binary.BigEndian, a.ipAddress)     // IP address
	binary.Write(buf, binary.BigEndian, a.key)           // Key
	binary.Write(buf, binary.BigEndian, a.numWant)       // Num want
	binary.Write(buf, binary.BigEndian, a.port)          // Port
	_, err = w.Write(buf.Bytes())
	return
}

type announceResponse struct {
	action        int32
	transactionId uint32
	interval      int32
	leechers      int32
	seeders       int32
	peers         []*libtorrent.PeerAddress
}

func parseAnnounceResponse(r io.Reader) (annRes *announceResponse, err error) {
	// Set byte size to equivalent of getting 150 peers
	b := make([]byte, 20+6*150)
	n, err := r.Read(b)
	if err != nil {
		return
	} else if n < 20 {
		err = stackerr.New("parseAnnounceResponse: response was less than 16 bytes")
		return
	}
	buf := bytes.NewBuffer(b)

	annRes = new(announceResponse)

	binary.Read(buf, binary.BigEndian, &annRes.action)
	binary.Read(buf, binary.BigEndian, &annRes.transactionId)
	binary.Read(buf, binary.BigEndian, &annRes.interval)
	binary.Read(buf, binary.BigEndian, &annRes.leechers)
	binary.Read(buf, binary.BigEndian, &annRes.seeders)

	for i := 0; i < (n-20)/6; i++ {
		var a, b, c, d uint8
		var port uint16

		binary.Read(buf, binary.BigEndian, &a)
		binary.Read(buf, binary.BigEndian, &b)
		binary.Read(buf, binary.BigEndian, &c)
		binary.Read(buf, binary.BigEndian, &d)
		binary.Read(buf, binary.BigEndian, &port)

		annRes.peers = append(annRes.peers, &libtorrent.PeerAddress{
			Host: fmt.Sprintf("%d.%d.%d.%d", a, b, c, d),
			Port: port,
		})
	}

	return
}
