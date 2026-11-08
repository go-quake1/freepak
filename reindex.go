package freepak

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"path"
	"strings"
)

// Every reindexer rewrites palette indices in place and never changes a size:
// the files keep their layout, only texel bytes move through the [Remap].
// Each one checks every offset and size it follows and returns an error rather
// than guessing.

var le = binary.LittleEndian

func i32(b []byte, off int) (int, error) {
	if off < 0 || off+4 > len(b) {
		return 0, errors.New("short data")
	}
	return int(int32(le.Uint32(b[off:]))), nil
}

// span returns b[off:off+n] or an error when it is out of range.
func span(b []byte, off, n int) ([]byte, error) {
	if off < 0 || n < 0 || off > len(b) || n > len(b)-off {
		return nil, fmt.Errorf("range %d+%d exceeds %d bytes", off, n, len(b))
	}
	return b[off : off+n : off+n], nil
}

// ReindexPic rewrites a pic: int32 width, int32 height, width*height texels.
func ReindexPic(b []byte, m *Remap) error {
	w, err := i32(b, 0)
	if err != nil {
		return err
	}
	h, err := i32(b, 4)
	if err != nil {
		return err
	}
	if w < 0 || h < 0 || w > 1<<15 || h > 1<<15 {
		return errors.New("pic: bad size")
	}
	px, err := span(b, 8, w*h)
	if err != nil {
		return err
	}
	if len(b) != 8+w*h {
		return fmt.Errorf("pic: %d bytes for %dx%d", len(b), w, h)
	}
	m.Apply(px)
	return nil
}

// reindexMiptex rewrites the mip levels of one miptex at b (name[16], width,
// height, 4 offsets relative to b). Each level is reindexed on its own, see
// ReindexBSP.
func reindexMiptex(b []byte, m *Remap) error {
	w, err := i32(b, 16)
	if err != nil {
		return err
	}
	h, err := i32(b, 20)
	if err != nil {
		return err
	}
	if w <= 0 || h <= 0 || w > 1<<12 || h > 1<<12 || w%8 != 0 || h%8 != 0 {
		return fmt.Errorf("miptex: bad size %dx%d", w, h)
	}
	for lvl := 0; lvl < 4; lvl++ {
		ofs, err := i32(b, 24+4*lvl)
		if err != nil {
			return err
		}
		if ofs == 0 { // external texture: no pixels here
			continue
		}
		px, err := span(b, ofs, (w>>lvl)*(h>>lvl))
		if err != nil {
			return fmt.Errorf("miptex mip %d: %w", lvl, err)
		}
		m.Apply(px)
	}
	return nil
}

// ReindexBSP rewrites the texture lump (lump 2) of a version 29 BSP in place,
// all four mip levels of every texture.
//
// Each mip is reindexed through the same table, not recomputed from level 0:
// the table preserves what the texel means at every level (fullbright stays
// fullbright, 255 stays transparent), the levels stay consistent with how they
// were authored, and no new averaging colour has to be quantised.
func ReindexBSP(bsp []byte, m *Remap) error {
	if len(bsp) < bspHeader || int32(le.Uint32(bsp)) != bspVersion {
		return errors.New("bsp: not a version 29 file")
	}
	ofs, _ := i32(bsp, 4+2*8)
	n, _ := i32(bsp, 8+2*8)
	lump, err := span(bsp, ofs, n)
	if err != nil {
		return fmt.Errorf("bsp texture lump: %w", err)
	}
	if len(lump) == 0 {
		return nil
	}
	num, err := i32(lump, 0)
	if err != nil || num < 0 || num > 1<<16 {
		return errors.New("bsp: bad texture count")
	}
	for i := 0; i < num; i++ {
		o, err := i32(lump, 4+4*i)
		if err != nil {
			return err
		}
		if o == -1 {
			continue
		}
		if o < 4+4*num || o > len(lump) {
			return fmt.Errorf("bsp: texture %d offset %d", i, o)
		}
		if err := reindexMiptex(lump[o:], m); err != nil {
			return fmt.Errorf("bsp texture %d: %w", i, err)
		}
	}
	return nil
}

// ReindexMDL rewrites the skins of an alias model (IDPO, version 6).
func ReindexMDL(b []byte, m *Remap) error {
	if len(b) < 84 || string(b[:4]) != "IDPO" || le.Uint32(b[4:]) != 6 {
		return errors.New("mdl: bad header")
	}
	nskins, _ := i32(b, 48)
	sw, _ := i32(b, 52)
	sh, _ := i32(b, 56)
	if nskins < 0 || nskins > 1<<12 || sw <= 0 || sh <= 0 || sw > 1<<12 || sh > 1<<12 {
		return errors.New("mdl: bad skin header")
	}
	size := sw * sh
	pos := 84
	for i := 0; i < nskins; i++ {
		group, err := i32(b, pos)
		if err != nil {
			return err
		}
		pos += 4
		count := 1
		if group != 0 {
			if count, err = i32(b, pos); err != nil || count <= 0 || count > 1<<12 {
				return errors.New("mdl: bad skin group")
			}
			pos += 4 + 4*count // intervals
		}
		for k := 0; k < count; k++ {
			px, err := span(b, pos, size)
			if err != nil {
				return fmt.Errorf("mdl skin %d: %w", i, err)
			}
			m.Apply(px)
			pos += size
		}
	}
	return nil
}

// ReindexSPR rewrites the frames of a sprite (IDSP, version 1).
func ReindexSPR(b []byte, m *Remap) error {
	if len(b) < 36 || string(b[:4]) != "IDSP" || le.Uint32(b[4:]) != 1 {
		return errors.New("spr: bad header")
	}
	nframes, _ := i32(b, 24)
	if nframes < 0 || nframes > 1<<12 {
		return errors.New("spr: bad frame count")
	}
	frame := func(pos int) (int, error) { // origin[2], width, height, texels
		w, err := i32(b, pos+8)
		if err != nil {
			return 0, err
		}
		h, err := i32(b, pos+12)
		if err != nil {
			return 0, err
		}
		if w < 0 || h < 0 || w > 1<<12 || h > 1<<12 {
			return 0, errors.New("spr: bad frame size")
		}
		px, err := span(b, pos+16, w*h)
		if err != nil {
			return 0, err
		}
		m.Apply(px)
		return pos + 16 + w*h, nil
	}
	pos := 36
	for i := 0; i < nframes; i++ {
		group, err := i32(b, pos)
		if err != nil {
			return err
		}
		pos += 4
		count := 1
		if group != 0 {
			if count, err = i32(b, pos); err != nil || count <= 0 || count > 1<<12 {
				return errors.New("spr: bad group")
			}
			pos += 4 + 4*count
		}
		for k := 0; k < count; k++ {
			if pos, err = frame(pos); err != nil {
				return fmt.Errorf("spr frame %d: %w", i, err)
			}
		}
	}
	return nil
}

// WAD2 lump types (id's wad.h): palette, qpic, miptex; conchars is a raw
// 128x128 picture stored under the miptex type, recognised by its name.
const (
	wadPalette = 64
	wadQPic    = 66
	wadMipTex  = 68
)

// ReindexWAD rewrites the pictures of a WAD2 archive in place and replaces a
// palette lump by pal. Unknown lump types are an error.
func ReindexWAD(b []byte, m *Remap, pal []byte) error {
	if len(b) < 12 || string(b[:4]) != "WAD2" {
		return errors.New("wad: bad header")
	}
	n, _ := i32(b, 4)
	dir, _ := i32(b, 8)
	if n < 0 || n > 1<<16 {
		return errors.New("wad: bad lump count")
	}
	info, err := span(b, dir, n*32)
	if err != nil {
		return fmt.Errorf("wad directory: %w", err)
	}
	for i := 0; i < n; i++ {
		e := info[i*32:][:32]
		pos, size, typ := int(int32(le.Uint32(e))), int(int32(le.Uint32(e[8:]))), e[12]
		if e[13] != 0 {
			return errors.New("wad: compressed lump")
		}
		name := string(bytes.TrimRight(e[16:32], "\x00"))
		lump, err := span(b, pos, size)
		if err != nil {
			return fmt.Errorf("wad lump %s: %w", name, err)
		}
		switch {
		case typ == wadPalette:
			if len(lump) != 768 {
				return fmt.Errorf("wad palette lump of %d bytes", len(lump))
			}
			copy(lump, pal)
		case typ == wadQPic:
			err = ReindexPic(lump, m)
		case typ == wadMipTex && strings.EqualFold(name, "conchars"):
			if len(lump) != 128*128 {
				return fmt.Errorf("wad conchars of %d bytes", len(lump))
			}
			m.Apply(lump)
		case typ == wadMipTex:
			err = reindexMiptex(lump, m)
		default:
			err = fmt.Errorf("unknown lump type %d", typ)
		}
		if err != nil {
			return fmt.Errorf("wad lump %s: %w", name, err)
		}
	}
	return nil
}

// ReindexEntry reindexes the entry called name when it holds indexed pixels
// and reports whether it did. data must be a private copy: it is modified.
func ReindexEntry(name string, data []byte, m *Remap, pal []byte) (bool, error) {
	var err error
	m.Err, m.seen = nil, nil
	defer func() { m.seen = nil }()
	switch {
	case name == "gfx.wad":
		err = ReindexWAD(data, m, pal)
	case name == "gfx/palette.lmp" || name == "gfx/colormap.lmp":
		return false, nil
	case strings.HasPrefix(name, "gfx/") && strings.HasSuffix(name, ".lmp"):
		err = ReindexPic(data, m)
	case path.Dir(name) == "maps" && strings.HasSuffix(name, ".bsp"):
		err = ReindexBSP(data, m)
	case strings.HasSuffix(name, ".mdl"):
		err = ReindexMDL(data, m)
	case strings.HasSuffix(name, ".spr"):
		err = ReindexSPR(data, m)
	default:
		return false, nil
	}
	if err == nil {
		err = m.Err
	}
	if err != nil {
		return false, fmt.Errorf("%s: %w", name, err)
	}
	return true, nil
}
