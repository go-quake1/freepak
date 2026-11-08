package freepak

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	bspVersion = 29
	numLumps   = 15
	bspHeader  = 4 + numLumps*8
)

// RenameChangelevels rewrites the "map" key of the entity lump of a version 29
// BSP: every `"map" "<from>"` becomes `"map" "<to>"` for the pairs in rename.
// The other lumps are copied byte for byte and re-laid out on 4-byte
// boundaries. It returns the new file and the number of keys rewritten; with
// zero rewrites the input is returned unchanged.
func RenameChangelevels(bsp []byte, rename map[string]string) ([]byte, int, error) {
	if len(bsp) < bspHeader || int32(binary.LittleEndian.Uint32(bsp)) != bspVersion {
		return nil, 0, errors.New("bsp: not a version 29 file")
	}
	type lump struct{ ofs, n int }
	var lumps [numLumps]lump
	for i := range lumps {
		o := int64(int32(binary.LittleEndian.Uint32(bsp[4+i*8:])))
		n := int64(int32(binary.LittleEndian.Uint32(bsp[8+i*8:])))
		if o < bspHeader || n < 0 || o+n > int64(len(bsp)) {
			return nil, 0, fmt.Errorf("bsp: lump %d out of range", i)
		}
		lumps[i] = lump{int(o), int(n)}
	}
	ents := bsp[lumps[0].ofs : lumps[0].ofs+lumps[0].n]
	total := 0
	for from, to := range rename {
		old := []byte(`"map" "` + from + `"`)
		neu := []byte(`"map" "` + to + `"`)
		total += bytes.Count(ents, old)
		ents = bytes.ReplaceAll(ents, old, neu)
	}
	if total == 0 {
		return bsp, 0, nil
	}
	out := make([]byte, bspHeader, len(bsp)+64)
	binary.LittleEndian.PutUint32(out, bspVersion)
	for i := range lumps {
		data := bsp[lumps[i].ofs : lumps[i].ofs+lumps[i].n]
		if i == 0 {
			data = ents
		}
		for len(out)%4 != 0 {
			out = append(out, 0)
		}
		binary.LittleEndian.PutUint32(out[4+i*8:], uint32(len(out)))
		binary.LittleEndian.PutUint32(out[8+i*8:], uint32(len(data)))
		out = append(out, data...)
	}
	return out, total, nil
}
