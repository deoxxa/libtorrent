package main

import (
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"

	"github.com/funkygao/golib/profile"
	"github.com/torrance/libtorrent"
	// "github.com/torrance/libtorrent/peersource/nullsource"
	"github.com/torrance/libtorrent/peersource/trackers"
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
		PeerSourceFactories: []libtorrent.PeerSourceFactory{
			// libtorrent.PeerSourceFactory{
			// 	Constructor: nullsource.NewNullSource,
			// 	Config:      nil,
			// },
			libtorrent.PeerSourceFactory{
				Constructor: trackers.NewTrackers,
				Config: trackers.Config{
					Transports: map[string]libtorrent.TrackerTransportFactory{
						"http": libtorrent.TrackerTransportFactory{
							Constructor: http.NewTransport,
							Config:      nil,
						},
						"https": libtorrent.TrackerTransportFactory{
							Constructor: http.NewTransport,
							Config:      nil,
						},
						"udp": libtorrent.TrackerTransportFactory{
							Constructor: udp.NewTransport,
							Config:      nil,
						},
					},
				},
			},
		},
	}

	f, err := os.Open(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}

	m, err := libtorrent.ParseMetainfo(f)
	if err != nil {
		log.Fatal(err)
	}

	s, err := libtorrent.NewSession(m, &c)
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

	for {
		if s.State() != libtorrent.STATE_LEARNING {
			break
		}

		time.Sleep(time.Second * 1)
	}

	for {
		if s.State() != libtorrent.STATE_LEECHING {
			break
		}

		time.Sleep(time.Second * 1)
	}

	log.Printf("all done!")
}
