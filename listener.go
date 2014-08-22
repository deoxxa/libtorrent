package libtorrent

import (
	"fmt"
	"net"

	"github.com/facebookgo/stackerr"
)

type ListenerConfig struct {
	PeerId [20]byte
	Port   uint16
}

type Listener struct {
	errors   chan error
	messages chan string

	peerId   [20]byte
	port     uint16
	sessions map[[20]byte]*Session
	listener net.Listener
}

func NewListener(config ListenerConfig) *Listener {
	return &Listener{
		errors:   make(chan error, 100),
		messages: make(chan string, 100),
		peerId:   config.PeerId,
		port:     config.Port,
		sessions: make(map[[20]byte]*Session),
	}
}

func (l *Listener) Port() uint16 {
	return l.port
}

func (l *Listener) PeerId() [20]byte {
	return l.peerId
}

func (l *Listener) Errors() chan error {
	return l.errors
}

func (l *Listener) Messages() chan string {
	return l.messages
}

func (l *Listener) Sessions() []*Session {
	r := make([]*Session, len(l.sessions))

	i := 0
	for _, s := range l.sessions {
		r[i] = s
		i++
	}

	return r
}

func (l *Listener) GetSession(infoHash [20]byte) *Session {
	s, _ := l.sessions[infoHash]

	return s
}

func (l *Listener) AddSession(s *Session) {
	l.sessions[s.InfoHash()] = s

	go func() {
		for err := range s.Errors() {
			l.errors <- err
		}
	}()

	go func() {
		for message := range s.Messages() {
			l.messages <- message
		}
	}()
}

func (l *Listener) Listen() error {
	port := fmt.Sprintf(":%d", l.port)

	if listener, err := net.Listen("tcp", port); err != nil {
		return stackerr.Wrap(err)
	} else {
		l.listener = listener
	}

	return nil
}

func (l *Listener) Serve() error {
	for {
		conn, err := l.listener.Accept()
		if err != nil {
			return err
		}

		go func() {
			if hs, err := parseHandshake(conn); err != nil {
				l.errors <- stackerr.Wrap(err)
				conn.Close()
				return
			} else {
				if tor, ok := l.sessions[hs.infoHash]; ok {
					tor.AddPeer(conn, hs)
				} else {
					l.errors <- stackerr.New("incoming peer connection using invalid infohash")
					conn.Close()
				}
			}
		}()
	}

	return nil
}

func (l *Listener) ListenAndServe() error {
	if err := l.Listen(); err != nil {
		return err
	}

	return l.Serve()
}

func (l *Listener) Close() error {
	if err := l.listener.Close(); err != nil {
		return stackerr.Wrap(err)
	}

	return nil
}
