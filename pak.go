// Package freepak builds a freely licensed replacement for id Software's
// shareware id1/pak0.pak: the same paths, none of id's bytes.
//
// The PACK format is the one of Quake's pak files: a 12-byte header
// ("PACK", directory offset, directory length) and a directory of 64-byte
// entries (56-byte NUL-padded name, file offset, file length), little endian.
package freepak

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

const (
	headerSize = 12
	entrySize  = 64
	nameSize   = 56
	// maxPak bounds what ReadPak accepts: a pak offset is an int32.
	maxPak = 1<<31 - 1
)

// File is one entry of a pak.
type File struct {
	Name string
	Data []byte
}

// ReadPak parses a PACK archive held in memory. Every offset and length is
// checked against the archive size; names must be non-empty, printable ASCII
// without a leading slash, a ".." element or a backslash.
func ReadPak(b []byte) ([]File, error) {
	if len(b) < headerSize || len(b) > maxPak {
		return nil, errors.New("pak: bad size")
	}
	if string(b[:4]) != "PACK" {
		return nil, errors.New("pak: bad magic")
	}
	dirOfs := int64(int32(binary.LittleEndian.Uint32(b[4:])))
	dirLen := int64(int32(binary.LittleEndian.Uint32(b[8:])))
	if dirOfs < headerSize || dirLen < 0 || dirLen%entrySize != 0 || dirOfs+dirLen > int64(len(b)) {
		return nil, errors.New("pak: bad directory")
	}
	n := int(dirLen / entrySize)
	files := make([]File, 0, n)
	seen := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		e := b[dirOfs+int64(i)*entrySize:][:entrySize]
		name := e[:nameSize]
		if k := bytes.IndexByte(name, 0); k >= 0 {
			name = name[:k]
		}
		if err := checkName(string(name)); err != nil {
			return nil, fmt.Errorf("pak: entry %d: %w", i, err)
		}
		ofs := int64(int32(binary.LittleEndian.Uint32(e[nameSize:])))
		ln := int64(int32(binary.LittleEndian.Uint32(e[nameSize+4:])))
		if ofs < headerSize || ln < 0 || ofs+ln > int64(len(b)) {
			return nil, fmt.Errorf("pak: entry %q out of range", name)
		}
		if seen[string(name)] {
			return nil, fmt.Errorf("pak: duplicate entry %q", name)
		}
		seen[string(name)] = true
		files = append(files, File{Name: string(name), Data: b[ofs : ofs+ln : ofs+ln]})
	}
	return files, nil
}

func checkName(s string) error {
	if s == "" || len(s) >= nameSize {
		return errors.New("bad name length")
	}
	if s[0] == '/' || strings.Contains(s, "\\") {
		return fmt.Errorf("bad name %q", s)
	}
	for _, el := range strings.Split(s, "/") {
		if el == "" || el == ".." || el == "." {
			return fmt.Errorf("bad name %q", s)
		}
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return fmt.Errorf("bad name %q", s)
		}
	}
	return nil
}

// WritePak serialises files in the given order. The result depends only on
// the names and contents, so equal inputs give identical bytes.
func WritePak(files []File) ([]byte, error) {
	size := int64(headerSize) + int64(len(files))*entrySize
	for _, f := range files {
		if err := checkName(f.Name); err != nil {
			return nil, err
		}
		size += int64(len(f.Data))
	}
	if size > maxPak {
		return nil, errors.New("pak: archive would exceed 2 GiB")
	}
	out := make([]byte, headerSize, size)
	copy(out, "PACK")
	ofs := make([]int32, len(files))
	for i, f := range files {
		ofs[i] = int32(len(out))
		out = append(out, f.Data...)
	}
	binary.LittleEndian.PutUint32(out[4:], uint32(len(out)))
	binary.LittleEndian.PutUint32(out[8:], uint32(len(files)*entrySize))
	for i, f := range files {
		var e [entrySize]byte
		copy(e[:nameSize], f.Name)
		binary.LittleEndian.PutUint32(e[nameSize:], uint32(ofs[i]))
		binary.LittleEndian.PutUint32(e[nameSize+4:], uint32(len(f.Data)))
		out = append(out, e[:]...)
	}
	return out, nil
}
