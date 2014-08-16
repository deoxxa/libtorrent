package libtorrent

import (
	"fmt"
	"net"

	"github.com/facebookgo/stackerr"
)

type Listener struct {
	Errors chan error

	port     uint16
	sessions map[[20]byte]*Session
	listener net.Listener
}

func NewListener(port uint16) *Listener {
	return &Listener{
		Errors:   make(chan error, 100),
		port:     port,
		sessions: make(map[[20]byte]*Session),
	}
}

func (l *Listener) AddSession(s *Session) {
	l.sessions[s.InfoHash()] = s
}

func (l *Listener) Listen() error {
	port := fmt.Sprintf(":%d", l.port)

	if listener, err := net.Listen("tcp", port); err != nil {
		return stackerr.Wrap(err)
	} else {
		l.listener = listener
	}

	// Begin accepting incoming peers
	go func() {
		for {
			conn, err := l.listener.Accept()
			if err != nil {
				l.Errors <- stackerr.Wrap(err)
				break
			}

			go func() {
				if hs, err := parseHandshake(conn); err != nil {
					l.Errors <- stackerr.Wrap(err)
					conn.Close()
					return
				} else {
					if tor, ok := l.sessions[hs.infoHash]; ok {
						tor.AddPeer(conn, hs)
					} else {
						l.Errors <- stackerr.Newf("%s Incoming peer connection using expired/invalid infohash", conn.RemoteAddr())
						conn.Close()
					}
				}
			}()
		}
	}()

	return nil
}

func (l *Listener) Close() error {
	if err := l.listener.Close(); err != nil {
		return stackerr.Wrap(err)
	}

	return nil
}
