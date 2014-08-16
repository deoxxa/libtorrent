package http

import (
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/facebookgo/stackerr"
	"github.com/torrance/libtorrent"
	"github.com/torrance/libtorrent/tracker"
	"github.com/zeebo/bencode"
)

type Transport struct {
	u *url.URL
}

var EventNames = []string{"empty", "started", "completed", "stopped"}

func NewTransport(u *url.URL, config interface{}) (tracker.Transport, error) {
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, stackerr.New("scheme is unrecognised")
	}

	t := &Transport{
		u: u,
	}

	return t, nil
}

type AnnounceResponse struct {
	FailureReason   string                    `bencode:"failure reason"`
	Downloaded      uint32                    `bencode:"downloaded"`
	Incomplete      uint32                    `bencode:"incomplete"`
	Complete        uint32                    `bencode:"complete"`
	Interval        int                       `bencode:"interval"`
	MinimumInterval int                       `bencode:"min interval"`
	RawPeers        interface{}               `bencode:"peers"`
	Peers           []*libtorrent.PeerAddress `bencode:"-"`
}

func (t *Transport) Announce(req *tracker.AnnounceRequest) (*tracker.AnnounceResponse, error) {
	u, err := url.Parse(t.u.String())
	if err != nil {
		return nil, stackerr.Wrap(err)
	}

	q := u.Query()

	if req.Event != 0 {
		q.Set("event", EventNames[req.Event])
	}
	q.Set("info_hash", fmt.Sprintf("%s", req.InfoHash))
	q.Set("peer_id", fmt.Sprintf("%s", req.PeerId))
	if req.IP != [4]byte{0, 0, 0, 0} {
		q.Set("ip", fmt.Sprintf("%d.%d.%d.%d", req.IP[0], req.IP[1], req.IP[2], req.IP[3]))
	}
	q.Set("port", fmt.Sprintf("%d", req.Port))
	q.Set("uploaded", fmt.Sprintf("%d", req.Uploaded))
	q.Set("downloaded", fmt.Sprintf("%d", req.Downloaded))
	q.Set("left", fmt.Sprintf("%d", req.Left))

	u.RawQuery = q.Encode()

	resp, err := http.Get(u.String())
	if err != nil {
		return nil, stackerr.Wrap(err)
	}
	if resp.StatusCode != 200 {
		return nil, stackerr.Newf("invalid status code; expected 200 but got %d", resp.StatusCode)
	}

	var ares AnnounceResponse
	d := bencode.NewDecoder(resp.Body)
	if err := d.Decode(&ares); err != nil {
		return nil, stackerr.Wrap(err)
	}

	if ares.FailureReason != "" {
		return nil, stackerr.Newf("error from tracker: %s", ares.FailureReason)
	}

	switch rp := ares.RawPeers.(type) {
	case string:
		if len(rp)%6 != 0 {
			return nil, stackerr.New("invalid compact peer data")
		}

		for i := 0; i < len(rp)/6; i++ {
			ares.Peers = append(ares.Peers, &libtorrent.PeerAddress{
				Host: fmt.Sprintf("%d.%d.%d.%d", rp[i*6+0], rp[i*6+1], rp[i*6+2], rp[i*6+3]),
				Port: uint16(rp[i*6+4]*255) + uint16(rp[i*6+5]),
			})
		}
	default:
		return nil, stackerr.New("invalid peer key type")
	}

	r := tracker.AnnounceResponse{
		Leechers:          ares.Incomplete,
		Seeders:           ares.Complete,
		Peers:             ares.Peers,
		RequiredInterval:  time.Duration(ares.MinimumInterval) * time.Second,
		SuggestedInterval: time.Duration(ares.Interval) * time.Second,
	}

	return &r, nil
}