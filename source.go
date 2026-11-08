package freepak

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// LibreQuakeSHA256 pins the only LibreQuake archive the build accepts:
// v0.09-beta lite.zip.
const LibreQuakeSHA256 = "428e736b2f01d953e09a08c60bee975bdc4a0ac2219e97fa095c8af41754da83"

// maxMember bounds what is read from one zip member.
const maxMember = 256 << 20

// licence documents carried from the LibreQuake archive into the release.
var licenceDocs = []string{
	"docs/COPYING",
	"docs/CREDITS",
	"docs/README-IMPORTANT-LICENCE-INFO",
	"docs/misc-docs/COPYING",
	"docs/misc-docs/LibreProgsDeluxe.md",
}

// LibreQuake is the content read from the pinned archive.
type LibreQuake struct {
	// Files is pak0 overlaid by pak1 (a later pak wins, as in Quake), in a
	// stable order: pak0's entries, then pak1's new ones.
	Files []File
	// Docs maps a path under id1/ (see licenceDocs) to its bytes.
	Docs map[string][]byte
}

// Lookup returns the entry called name.
func (lq *LibreQuake) Lookup(name string) ([]byte, bool) {
	for _, f := range lq.Files {
		if f.Name == name {
			return f.Data, true
		}
	}
	return nil, false
}

// OpenLibreQuake reads path, refuses it unless its sha256 is
// [LibreQuakeSHA256], and extracts the two paks and the licence documents.
func OpenLibreQuake(path string) (*LibreQuake, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	if got := hex.EncodeToString(sum[:]); got != LibreQuakeSHA256 {
		return nil, fmt.Errorf("librequake: %s has sha256 %s, want the pinned %s", path, got, LibreQuakeSHA256)
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, err
	}
	member := func(name string) ([]byte, error) {
		for _, f := range zr.File {
			if f.Name != "lite/id1/"+name {
				continue
			}
			if f.UncompressedSize64 > maxMember {
				return nil, fmt.Errorf("librequake: %s is too large", name)
			}
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			b, err := io.ReadAll(io.LimitReader(rc, maxMember+1))
			if err != nil {
				return nil, err
			}
			if len(b) > maxMember {
				return nil, fmt.Errorf("librequake: %s is too large", name)
			}
			return b, nil
		}
		return nil, fmt.Errorf("librequake: %s not in the archive", name)
	}
	lq := &LibreQuake{Docs: map[string][]byte{}}
	index := map[string]int{}
	for _, p := range []string{"pak0.pak", "pak1.pak"} {
		b, err := member(p)
		if err != nil {
			return nil, err
		}
		files, err := ReadPak(b)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		for _, f := range files {
			if i, ok := index[f.Name]; ok {
				lq.Files[i] = f
				continue
			}
			index[f.Name] = len(lq.Files)
			lq.Files = append(lq.Files, f)
		}
	}
	for _, d := range licenceDocs {
		b, err := member(d)
		if err != nil {
			return nil, err
		}
		lq.Docs[d] = b
	}
	return lq, nil
}
