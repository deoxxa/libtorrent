package tree

import (
	"errors"
	"io"

	"github.com/facebookgo/stackerr"
	"github.com/torrance/libtorrent"
)

type NodeConstructor func(name string, length int64, config interface{}) (Node, error)

type NodeFactory struct {
	Constructor NodeConstructor
	Config      interface{}
}

type Config struct {
	NodeFactory NodeFactory
}

type Tree struct {
	*libtorrent.BaseStore

	config Config
	nodes  []Node
}

type Node interface {
	io.ReaderAt
	io.WriterAt
	Length() int64
}

func NewTree(m *libtorrent.Metainfo, config interface{}) (libtorrent.Store, error) {
	c, ok := config.(Config)
	if !ok {
		return nil, errors.New("config must be a tree.Config")
	}

	t := &Tree{
		BaseStore: libtorrent.NewBaseStore(m),
	}

	for _, file := range m.Files {
		if node, err := c.NodeFactory.Constructor(file.Path, file.Length, c.NodeFactory.Config); err != nil {
			return nil, stackerr.Wrap(err)
		} else {
			t.nodes = append(t.nodes, node)
		}
	}

	return t, nil
}

func (s *Tree) GetBlock(index int, offset int64, block []byte) (int, error) {
	if offset+int64(len(block)) > s.GetSize(index) {
		return 0, errors.New("Requested block overran piece length")
	}

	totalOffset := s.GetOffset(index) + offset
	seek := totalOffset
	n := 0

	for _, node := range s.nodes {
		if seek > node.Length() {
			seek -= node.Length()
			continue
		}

		end := n + int(node.Length())
		if n+end > len(block) {
			end = len(block)
		}

		if _n, err := node.ReadAt(block[n:end], seek); err == nil {
			n += _n
			break
		} else if err == io.EOF {
			n += _n
			seek = 0
		} else if err != nil {
			return n + _n, stackerr.Wrap(err)
		}
	}

	return n, nil
}

func (s *Tree) SetBlock(index int, offset int64, block []byte) (int, error) {
	if offset+int64(len(block)) > s.GetSize(index) {
		return 0, errors.New("Requested block overran piece length")
	}

	totalOffset := s.GetOffset(index) + offset
	seek := totalOffset
	n := 0

	for _, node := range s.nodes {
		if seek > node.Length() {
			seek -= node.Length()
			continue
		}

		end := n + int(node.Length())
		if n+end > len(block) {
			end = len(block)
		}

		if _n, err := node.WriteAt(block[n:end], seek); err == nil {
			n += _n
			break
		} else if err == io.EOF {
			n += _n
			seek = 0
		} else if err != nil {
			return n + _n, stackerr.Wrap(err)
		}
	}

	return n, nil
}
