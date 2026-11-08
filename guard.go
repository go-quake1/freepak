package freepak

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"slices"
)

// Window is the size of the byte window the no-copy gate compares: 4 KiB.
const Window = 4096

const hashBase uint64 = 0x100000001B3 // odd multiplier of the rolling hash

// rollingHashes calls fn(offset, hash) for the hash of every Window-byte
// window of b (polynomial hash mod 2^64), except windows of a single repeated
// byte value.
func rollingHashes(b []byte, fn func(off int, h uint64)) {
	if len(b) < Window {
		return
	}
	var pow uint64 = 1 // hashBase^(Window-1)
	for i := 1; i < Window; i++ {
		pow *= hashBase
	}
	var h uint64
	run := 0 // length of the run of equal bytes ending at i
	for i := 0; i < len(b); i++ {
		if i > 0 && b[i] == b[i-1] {
			run++
		} else {
			run = 1
		}
		if i >= Window {
			h = (h-(uint64(b[i-Window])+1)*pow)*hashBase + uint64(b[i]) + 1
		} else {
			h = h*hashBase + uint64(b[i]) + 1
		}
		// A window of one repeated byte (padding) carries no evidence of copying.
		if i >= Window-1 && run < Window {
			fn(i-Window+1, h)
		}
	}
}

// MaxSamePaletteColours is how many of the 256 palette entries may equal the
// reference's by value (the output palette is generated from its own
// parameters; pure black and white may coincide).
const MaxSamePaletteColours = 4

// CheckPalette fails if the output palette is the reference's or shares more
// than MaxSamePaletteColours entries with it; it returns the number shared.
func CheckPalette(out, ref []File) (int, error) {
	find := func(fs []File) []byte {
		for _, f := range fs {
			if f.Name == "gfx/palette.lmp" {
				return f.Data
			}
		}
		return nil
	}
	o, r := find(out), find(ref)
	if len(o) != 768 || len(r) != 768 {
		return 0, fmt.Errorf("palette.lmp has %d bytes in the output and %d in the reference, want 768", len(o), len(r))
	}
	same := 0
	for i := 0; i < 256; i++ {
		if bytes.Equal(o[i*3:i*3+3], r[i*3:i*3+3]) {
			same++
		}
	}
	if same > MaxSamePaletteColours {
		return same, fmt.Errorf("palette.lmp shares %d of 256 colours with the reference (at most %d allowed)", same, MaxSamePaletteColours)
	}
	return same, nil
}

// RefReport is what the no-id-bytes gate measured.
type RefReport struct {
	RefEntries  int    // entries of the reference pak
	RefSHA256   string // of the reference pak file, as given
	RefWindows  int    // 4 KiB windows hashed from the reference (never stored in output)
	OutEntries  int
	OutWindows  int // windows scanned in the output
	SameSHA     int // output entries whose sha256 equals a reference entry's
	WindowHits  int // output windows whose hash equals a reference window's
	SameColours int // palette entries equal to the reference's by value (CheckPalette)
}

// CheckNoReferenceBytes is the gate that no byte run of the reference pak is in
// out. It fails if an output entry has the sha256 of a reference entry, or if
// any 4 KiB window of any output entry hashes like a window of any reference
// entry (every offset of both sides, so any shared run of 4096 bytes or more is
// found). The reference hashes live in memory for the call only.
func CheckNoReferenceBytes(out, ref []File, refSHA string) (RefReport, error) {
	r := RefReport{RefEntries: len(ref), RefSHA256: refSHA, OutEntries: len(out)}
	refSums := make(map[[32]byte]string, len(ref))
	var wins []uint64
	for _, f := range ref {
		refSums[sha256.Sum256(f.Data)] = f.Name
		rollingHashes(f.Data, func(_ int, h uint64) { wins = append(wins, h) })
	}
	slices.Sort(wins)
	wins = slices.Compact(wins)
	r.RefWindows = len(wins)
	var bad []string
	for _, f := range out {
		if name, ok := refSums[sha256.Sum256(f.Data)]; ok {
			r.SameSHA++
			bad = append(bad, fmt.Sprintf("%s is byte-identical to reference %s", f.Name, name))
		}
		rollingHashes(f.Data, func(off int, h uint64) {
			r.OutWindows++
			if _, found := slices.BinarySearch(wins, h); found {
				r.WindowHits++
				if len(bad) < 20 {
					bad = append(bad, fmt.Sprintf("%s offset %d: a %d-byte window matches the reference", f.Name, off, Window))
				}
			}
		})
	}
	if len(bad) > 0 {
		return r, fmt.Errorf("output contains reference bytes: %v", bad)
	}
	return r, nil
}
