package tree

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type DiskNodeConfig struct {
	Base string
}

type DiskNode struct {
	name   string
	length int64
	fd     *os.File
}

func NewDiskNode(name string, length int64, config interface{}) (Node, error) {
	c, ok := config.(DiskNodeConfig)
	if !ok {
		return nil, errors.New("couldn't use config object")
	}

	if len(name) == 0 {
		return nil, errors.New("Path must have at least 1 component.")
	}

	// Root directory must already exist
	baseFileInfo, err := os.Stat(c.Base)
	if err != nil {
		return nil, err
	}
	if !baseFileInfo.IsDir() {
		return nil, errors.New(c.Base + " is not a directory")
	}

	absPath := filepath.Join(c.Base, name)

	// Create any required parent directories
	dirs := filepath.Dir(absPath)
	if err = os.MkdirAll(dirs, 0755); err != nil {
		return nil, err
	}

	// Create or open file
	fd, err := os.OpenFile(absPath, os.O_RDWR|os.O_CREATE, 0644)

	// Stat for size of file
	stat, err := fd.Stat()
	if err != nil {
		return nil, err
	}
	if length-stat.Size() < 0 {
		return nil, errors.New("Node already exists and is larger than expected size. Aborting.")
	}

	// Now pad the file from the end until it matches required size
	err = fd.Truncate(length)
	if err != nil {
		return nil, err
	}

	file := &DiskNode{
		name:   name,
		length: length,
		fd:     fd,
	}

	return file, nil
}

func (d *DiskNode) ReadAt(p []byte, off int64) (n int, err error) {
	return d.fd.ReadAt(p, off)
}

func (d *DiskNode) WriteAt(p []byte, off int64) (n int, err error) {
	return d.fd.WriteAt(p, off)
}

func (d *DiskNode) Length() int64 {
	return d.length
}

func (d *DiskNode) String() string {
	return fmt.Sprintf("[Name: %s, Length: %d bytes]", d.name, d.length)
}
