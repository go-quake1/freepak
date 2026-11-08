package freepak

import (
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

// FunctionalExceptions are the entries that may equal the reference's because
// no other bytes would work, each with the reason. The gate stamps them in the
// MANIFEST; it accepts nothing else.
var FunctionalExceptions = map[string]string{
	"gfx/palette.lmp": "the 256-colour palette: 768 bytes of RGB values that every Quake asset (textures, models, sprites, pictures, colormap) is indexed against, " +
		"and that LibreQuake itself ships; a different palette would recolour every asset",
}

// RefReport is what the no-id-bytes gate measured.
type RefReport struct {
	RefEntries int    // entries of the reference pak
	RefSHA256  string // of the reference pak file, as given
	RefWindows int    // 4 KiB windows hashed from the reference (never stored in output)
	OutEntries int
	OutWindows int      // windows scanned in the output
	SameSHA    int      // output entries whose sha256 equals a reference entry's and are not excepted
	Excepted   []string // identical entries that FunctionalExceptions allows, by path
	WindowHits int      // output windows whose hash equals a reference window's
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
			if _, excepted := FunctionalExceptions[f.Name]; excepted && name == f.Name {
				r.Excepted = append(r.Excepted, f.Name)
			} else {
				r.SameSHA++
				bad = append(bad, fmt.Sprintf("%s is byte-identical to reference %s", f.Name, name))
			}
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
