package main

import (
	"encoding/base32"
	"encoding/hex"
	nhttp "net/http"
	"net/url"
	"os"

	"github.com/facebookgo/stackerr"
	"github.com/torrance/libtorrent"
	"github.com/torrance/libtorrent/store/tree"
	"github.com/torrance/libtorrent/tracker/http"
	"github.com/torrance/libtorrent/tracker/udp"
	"github.com/wsxiaoys/terminal/color"
)

type ClientConfig struct {
	PeerId [20]byte
	Port   uint16
	Path   string
}

type Client struct {
	peerId [20]byte
	port   uint16
	path   string

	listener          *libtorrent.Listener
	storeFactory      libtorrent.StoreFactory
	peerSources       []libtorrent.PeerSourceFactory
	trackerTransports map[string]libtorrent.TrackerTransportFactory
}

func NewClient(config ClientConfig) *Client {
	return &Client{
		peerId: config.PeerId,
		port:   config.Port,
		path:   config.Path,
		listener: libtorrent.NewListener(libtorrent.ListenerConfig{
			PeerId: config.PeerId,
			Port:   config.Port,
		}),
		storeFactory: libtorrent.StoreFactory{
			Constructor: tree.NewTree,
			Config: tree.Config{
				NodeFactory: tree.NodeFactory{
					Constructor: tree.NewDiskNode,
					Config: tree.DiskNodeConfig{
						Base: os.Args[1],
					},
				},
			},
		},
		peerSources: []libtorrent.PeerSourceFactory{},
		trackerTransports: map[string]libtorrent.TrackerTransportFactory{
			"http": {
				Constructor: http.NewTransport,
				Config:      nil,
			},
			"https": {
				Constructor: http.NewTransport,
				Config:      nil,
			},
			"udp": {
				Constructor: udp.NewTransport,
				Config:      nil,
			},
		},
	}
}

func (c *Client) Add(t string) (*libtorrent.Session, error) {
	config := libtorrent.Config{
		PeerId:            c.listener.PeerId(),
		Port:              c.listener.Port(),
		StoreFactory:      c.storeFactory,
		PeerSources:       c.peerSources,
		TrackerTransports: c.trackerTransports,
		Trackers:          []string{},
	}

	u, err := url.Parse(t)
	if err != nil {
		return nil, stackerr.Wrap(err)
	}

	switch u.Scheme {
	case "":
		if err := c.ConfigureLocalFile(u, &config); err != nil {
			return nil, stackerr.Wrap(err)
		}
	case "magnet":
		if err := c.ConfigureMagnetLink(u, &config); err != nil {
			return nil, stackerr.Wrap(err)
		}
	case "http":
		fallthrough
	case "https":
		if err := c.ConfigureHttpLink(u, &config); err != nil {
			return nil, stackerr.Wrap(err)
		}
	default:
		return nil, stackerr.New("can't interpret url")
	}

	s, err := libtorrent.NewSession(&config)
	if err != nil {
		return nil, stackerr.Wrap(err)
	}

	if err := s.Start(); err != nil {
		return nil, stackerr.Wrap(err)
	}

	c.listener.AddSession(s)

	go func() {
		for err := range s.Errors() {
			color.Println("@r" + err.Error())
		}
	}()

	go func() {
		for message := range s.Messages() {
			color.Println("@b" + message)
		}
	}()

	return s, nil
}

func (c *Client) ConfigureLocalFile(u *url.URL, config *libtorrent.Config) error {
	f, err := os.Open(u.Path)
	if err != nil {
		return stackerr.Wrap(err)
	}

	m, err := libtorrent.ParseMetainfo(f)
	if err != nil {
		return stackerr.Wrap(err)
	}

	config.Metainfo = m

	return nil
}

func (c *Client) ConfigureHttpLink(u *url.URL, config *libtorrent.Config) error {
	resp, err := nhttp.Get(u.String())
	if err != nil {
		return stackerr.Wrap(err)
	}

	m, err := libtorrent.ParseMetainfo(resp.Body)
	if err != nil {
		return stackerr.Wrap(err)
	}

	config.Metainfo = m

	return nil
}

func (c *Client) ConfigureMagnetLink(u *url.URL, config *libtorrent.Config) error {
	xt := u.Query().Get("xt")
	if len(xt) < 9 {
		return stackerr.New("malformed magnet uri")
	}

	if xt[0:9] != "urn:btih:" {
		return stackerr.New("unsupported exact topic form in magnet uri, expected urn:btih")
	}

	var infoHash []byte
	var err error

	if len(xt) == 41 {
		infoHash, err = base32.StdEncoding.DecodeString(xt[9:])
		if err != nil {
			return stackerr.Wrap(err)
		}
	} else if len(xt) == 49 {
		infoHash, err = hex.DecodeString(xt[9:])
		if err != nil {
			return stackerr.Wrap(err)
		}
	} else {
		return stackerr.New("malformed exact topic value")
	}

	copy(config.InfoHash[:], infoHash)

	config.Name = u.Query().Get("dn")

	if tr, ok := u.Query()["tr"]; ok {
		config.Trackers = append(config.Trackers, tr...)
	}

	return nil
}
