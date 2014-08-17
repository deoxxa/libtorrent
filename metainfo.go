package libtorrent

import (
	"bytes"
	"crypto/sha1"
	"io"
	"path/filepath"

	"github.com/facebookgo/stackerr"
	"github.com/zeebo/bencode"
)

type Metainfo struct {
	Name         string
	AnnounceList []string
	Pieces       [][20]byte
	PieceCount   int
	PieceLength  int64
	InfoHash     [20]byte
	Files        []struct {
		Length int64
		Path   string
	}
}

type InfoDict struct {
	Length      int64
	Name        string
	Pieces      []byte
	PieceLength int64 `bencode:"piece length"`
	Files       []struct {
		Length int64
		Path   []string
	}
}

func ParseInfoDict(b []byte) (*Metainfo, error) {
	var infoDict InfoDict

	dec := bencode.NewDecoder(bytes.NewReader(b))
	if err := dec.Decode(&infoDict); err != nil && err != io.EOF {
		return nil, stackerr.Wrap(err)
	}

	// Basic error checking
	if len(infoDict.Pieces)%20 != 0 {
		return nil, stackerr.New("Metainfo file malformed: Pieces length is not a multiple of 20.")
	}
	// TODO: Other error checking

	// Parse metaDecode into metainfo
	m := &Metainfo{
		Name:        infoDict.Name,
		PieceLength: infoDict.PieceLength,
		Pieces:      make([][20]byte, len(infoDict.Pieces)/20),
		PieceCount:  len(infoDict.Pieces) / 20,
	}

	// Pieces is a single string of concatenated 20-byte SHA1 hash values for all pieces in the torrent
	// Cycle through and create an slice of hashes
	for i := 0; i < len(infoDict.Pieces)/20; i++ {
		copy(m.Pieces[i][:], infoDict.Pieces[i*20:i*20+20])
	}

	// Single files and multiple files are stored differently. We normalise these into
	// a single description
	type file struct {
		Length int64
		Path   string
	}
	if len(infoDict.Files) == 0 && infoDict.Length != 0 {
		// Just one file
		m.Files = append(m.Files, file{Length: infoDict.Length, Path: infoDict.Name})
	} else {
		// Multiple files
		for _, f := range infoDict.Files {
			path := filepath.Join(append([]string{infoDict.Name}, f.Path...)...)
			m.Files = append(m.Files, file{Length: f.Length, Path: path})
		}
	}

	// Create infohash
	h := sha1.New()
	h.Write(b)
	d := h.Sum(nil)
	copy(m.InfoHash[:], d)

	return m, nil
}

func ParseMetainfo(r io.Reader) (*Metainfo, error) {
	var metaDecode struct {
		Announce string
		List     [][]string         `bencode:"announce-list"`
		RawInfo  bencode.RawMessage `bencode:"info"`
		Info     InfoDict
	}

	// We need the raw info data to derive the unique info_hash
	// of this torrent. Therefore we decode the metainfo in two steps
	// to obtain both the raw info data and its decoded form.
	dec := bencode.NewDecoder(r)
	if err := dec.Decode(&metaDecode); err != nil && err != io.EOF {
		return nil, stackerr.Wrap(err)
	}

	dec = bencode.NewDecoder(bytes.NewReader(metaDecode.RawInfo))
	if err := dec.Decode(&metaDecode.Info); err != nil && err != io.EOF {
		return nil, stackerr.Wrap(err)
	}

	// Basic error checking
	if len(metaDecode.Info.Pieces)%20 != 0 {
		return nil, stackerr.New("Metainfo file malformed: Pieces length is not a multiple of 20.")
	}
	// TODO: Other error checking

	// Parse metaDecode into metainfo
	m := &Metainfo{
		Name:        metaDecode.Info.Name,
		PieceLength: metaDecode.Info.PieceLength,
		Pieces:      make([][20]byte, len(metaDecode.Info.Pieces)/20),
		PieceCount:  len(metaDecode.Info.Pieces) / 20,
	}

	// Append other announce lists
	// First pass it through a map so we can ensure we have unique entries
	lists := make(map[string]bool)
	lists[metaDecode.Announce] = true // Primary announce list
	for _, tier := range metaDecode.List {
		for _, list := range tier {
			lists[list] = true
		}
	}
	// Now unravel map into list
	m.AnnounceList = make([]string, 0, len(lists))
	for list := range lists {
		m.AnnounceList = append(m.AnnounceList, list)
	}

	// Pieces is a single string of concatenated 20-byte SHA1 hash values for all pieces in the torrent
	// Cycle through and create an slice of hashes
	for i := 0; i < len(metaDecode.Info.Pieces)/20; i++ {
		copy(m.Pieces[i][:], metaDecode.Info.Pieces[i*20:i*20+20])
	}

	// Single files and multiple files are stored differently. We normalise these into
	// a single description
	type file struct {
		Length int64
		Path   string
	}
	if len(metaDecode.Info.Files) == 0 && metaDecode.Info.Length != 0 {
		// Just one file
		m.Files = append(m.Files, file{Length: metaDecode.Info.Length, Path: metaDecode.Info.Name})
	} else {
		// Multiple files
		for _, f := range metaDecode.Info.Files {
			path := filepath.Join(append([]string{metaDecode.Info.Name}, f.Path...)...)
			m.Files = append(m.Files, file{Length: f.Length, Path: path})
		}
	}

	// Create infohash
	h := sha1.New()
	h.Write(metaDecode.RawInfo)
	d := h.Sum(nil)
	copy(m.InfoHash[:], d)

	return m, nil
}
