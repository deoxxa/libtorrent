package libtorrent

import (
	"fmt"
	"net"
)

type Listener struct {
	port     uint16
	torrents map[string]*Torrent
	listener net.Listener
}

func NewListener(port uint16) (l *Listener) {
	l = &Listener{
		port:     port,
		torrents: make(map[string]*Torrent),
	}
	return
}

func (l *Listener) AddTorrent(tor *Torrent) {
	infoHash := fmt.Sprintf("%x", tor.InfoHash())
	l.torrents[infoHash] = tor
}

func (l *Listener) Listen() error {
	port := fmt.Sprintf(":%d", l.port)
	if listener, err := net.Listen("tcp", port); err != nil {
		return err
	} else {
		l.listener = listener
	}

	// Begin accepting incoming peers
	go func() {
		for {
			conn, err := l.listener.Accept()
			if err != nil {
				logger.Error(err.Error())
				break
			}

			go func() {
				hs, err := parseHandshake(conn)
				if err != nil {
					logger.Error(err.Error())
					conn.Close()
					return
				}

				infoHash := fmt.Sprintf("%x", hs.infoHash)
				if tor, ok := l.torrents[infoHash]; ok {
					logger.Debug("%s Incoming peer connection: %s", conn.RemoteAddr(), hs.peerId)
					tor.AddPeer(conn, hs)
				} else {
					logger.Info("%s Incoming peer connection using expired/invalid infohash", conn.RemoteAddr())
					conn.Close()
				}
				return
			}()
		}
	}()

	return nil
}

func (l *Listener) Close() error {
	return l.listener.Close()
}
