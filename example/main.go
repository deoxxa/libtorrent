package main

import (
	"encoding/base32"
	"encoding/hex"
	"fmt"
	"log"
	"math/rand"
	"net/url"
	"os"
	"time"

	"github.com/funkygao/golib/profile"
	"github.com/torrance/libtorrent"
	// "github.com/torrance/libtorrent/peersource/nullsource"
	// "github.com/torrance/libtorrent/peersource/trackers"
	"github.com/torrance/libtorrent/store/tree"
	"github.com/torrance/libtorrent/tracker/http"
	"github.com/torrance/libtorrent/tracker/udp"
	"github.com/wsxiaoys/terminal/color"
)

func main() {
	defer profile.Start(profile.CPUProfile).Stop()

	log.SetFlags(log.Lshortfile)

	rand.Seed(time.Now().UnixNano())

	if len(os.Args) < 3 {
		log.Fatal("usage: ./example /path/to/your.torrent /path/to/download/to")
	}

	port := uint16(20000 + rand.Intn(1000))

	l := libtorrent.NewListener(port)

	if err := l.Listen(); err != nil {
		log.Fatal(err)
	}

	peerId := [20]byte{}

	copy(peerId[:], fmt.Sprintf("-gtr-%x", rand.Int63()))

	log.Printf("peer id: %s", peerId)

	c := libtorrent.Config{
		PeerId: peerId,
		Port:   port,
		StoreFactory: libtorrent.StoreFactory{
			Constructor: tree.NewTree,
			Config: tree.Config{
				NodeFactory: tree.NodeFactory{
					Constructor: tree.NewDiskNode,
					Config: tree.DiskNodeConfig{
						Base: os.Args[2],
					},
				},
			},
		},
		PeerSources: []libtorrent.PeerSourceFactory{},
		TrackerTransports: map[string]libtorrent.TrackerTransportFactory{
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
		Trackers: []string{},
	}

	var m *libtorrent.Metainfo

	u, err := url.Parse(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}

	switch u.Scheme {
	case "":
		f, err := os.Open(u.Path)
		if err != nil {
			log.Fatal(err)
		}

		m, err = libtorrent.ParseMetainfo(f)
		if err != nil {
			log.Fatal(err)
		}
	case "magnet":
		xt := u.Query().Get("xt")
		if len(xt) < 9 {
			log.Fatal("malformed magnet uri")
		}

		if xt[0:9] != "urn:btih:" {
			log.Fatal("unsupported exact topic form in magnet uri, expected urn:btih")
		}

		var infoHash []byte

		if len(xt) == 41 {
			infoHash, err = base32.StdEncoding.DecodeString(xt[9:])
			if err != nil {
				log.Fatal(err)
			}
		} else if len(xt) == 49 {
			infoHash, err = hex.DecodeString(xt[9:])
			if err != nil {
				log.Fatal(err)
			}
		} else {
			log.Fatal("malformed exact topic value")
		}

		copy(c.InfoHash[:], infoHash)

		c.Name = u.Query().Get("dn")

		if tr, ok := u.Query()["tr"]; ok {
			c.Trackers = append(c.Trackers, tr...)
		}
	default:
		log.Fatal("can't interpret url")
	}

	s, err := libtorrent.NewSession(&c, m)
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		for err := range s.Errors() {
			color.Println("@r" + err.Error())
		}
	}()

	l.AddSession(s)

	s.Start()

	if s.State() == libtorrent.STATE_LEARNING {
		log.Printf("learning about torrent, need metadata")

		for {
			if s.State() != libtorrent.STATE_LEARNING {
				break
			}

			time.Sleep(time.Second * 1)
		}
	}

	log.Printf("beginning to download")

	for {
		if s.State() != libtorrent.STATE_LEECHING {
			break
		}

		time.Sleep(time.Second * 1)
	}

	log.Printf("all done!")
}
