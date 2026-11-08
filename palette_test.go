package freepak

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

func lightness(pal []byte, i int) float64 { return RGBToLab(pal[i*3], pal[i*3+1], pal[i*3+2]).L }

func TestOwnPaletteStructure(t *testing.T) {
	pal := OwnPalette()
	if len(pal) != 768 || !bytes.Equal(pal, OwnPalette()) {
		t.Fatal("size or determinism")
	}
	if pal[0] != 0 || pal[1] != 0 || pal[2] != 0 {
		t.Error("index 0 is not black")
	}
	// Every 16-entry block is a ramp in the direction the engine expects.
	for blk := 0; blk < 16; blk++ {
		first, last := lightness(pal, blk*16), lightness(pal, blk*16+15)
		darkToLight := blk < 8 || blk == 14
		if darkToLight != (last > first) {
			t.Errorf("block %d: lightness %.2f -> %.2f", blk, first, last)
		}
		for i := 1; i < 16; i++ {
			d := lightness(pal, blk*16+i) - lightness(pal, blk*16+i-1)
			if (d > 0) != darkToLight || d == 0 {
				t.Errorf("block %d entry %d is not strictly monotonic", blk, i)
			}
		}
	}
	// Fixed-index meanings: hue families of the blocks the engine hard-codes.
	hue := func(i int) float64 {
		l := RGBToLab(pal[i*3], pal[i*3+1], pal[i*3+2])
		h := math.Atan2(l.B, l.A) * 180 / math.Pi
		if h < 0 {
			h += 360
		}
		return h
	}
	chroma := func(i int) float64 {
		l := RGBToLab(pal[i*3], pal[i*3+1], pal[i*3+2])
		return math.Hypot(l.A, l.B)
	}
	for _, c := range []struct {
		idx      int
		lo, hi   float64
		what     string
		saturate bool
	}{
		{68, 15, 40, "red blood 67..70", true},
		{111, 40, 100, "fire ramp top 0x6f", true},
		{97, 15, 70, "fire ramp bottom 0x61", true},
		{54, 90, 140, "green band 52,54", true},
		{153, 330, 360, "magenta band 152..155", true},
		{230, 20, 90, "orange band 230,232", true},
	} {
		if h := hue(c.idx); h < c.lo || h > c.hi {
			t.Errorf("%s: index %d has hue %.0f, want %v..%v", c.what, c.idx, h, c.lo, c.hi)
		}
		if chroma(c.idx) < 0.04 {
			t.Errorf("%s: index %d is nearly grey", c.what, c.idx)
		}
	}
	for i := 0; i < 16; i++ { // greys
		if chroma(i) > 0.005 {
			t.Errorf("index %d is not grey", i)
		}
	}
}

func TestRemapKeepsEngineMeanings(t *testing.T) {
	// Any old palette will do for the class rules: use noise.
	old := noise(40, 768)
	m := NewRemap(old, OwnPalette())
	if m.Table[255] != 255 {
		t.Errorf("255 -> %d", m.Table[255])
	}
	black := make([]byte, 768) // an old palette whose index 0 is black, as LibreQuake's
	for i := 3; i < 768; i++ {
		black[i] = byte(40 + i%200)
	}
	if NewRemap(black, OwnPalette()).Table[0] != 0 {
		t.Error("black index 0 does not stay 0")
	}
	for old := 0; old < 256; old++ {
		n := int(m.Table[old])
		switch {
		case old == 255:
		case old >= 224:
			if n < 224 || n > 254 {
				t.Errorf("fullbright %d -> %d", old, n)
			}
		case old >= 16 && old < 32:
			if n < 16 || n >= 32 {
				t.Errorf("top range %d -> %d", old, n)
			}
		case old >= 96 && old < 112:
			if n < 96 || n >= 112 {
				t.Errorf("bottom range %d -> %d", old, n)
			}
		default:
			if n >= 224 || inTranslationBlock(n) {
				t.Errorf("plain colour %d -> %d (fullbright or translation range)", old, n)
			}
		}
	}
	// Identical palettes: every non-class colour maps to itself.
	own := OwnPalette()
	id := NewRemap(own, own)
	for i := 0; i < 255; i++ {
		if id.Table[i] != byte(i) && id.DeltaE[i] != 0 {
			t.Errorf("identity remap moves %d to %d", i, id.Table[i])
		}
	}
	if q := id.Stats(); q.Mean != 0 {
		t.Errorf("stats of nothing: %+v", q)
	}
}

func TestColormap(t *testing.T) {
	pal := OwnPalette()
	cm := Colormap(pal)
	if len(cm) != 16385 || !bytes.Equal(cm, Colormap(pal)) {
		t.Fatalf("%d bytes or not deterministic", len(cm))
	}
	if cm[16384] != 32 {
		t.Errorf("trailer %d, want 32", cm[16384])
	}
	for row := 0; row < 64; row++ {
		for c := 224; c < 256; c++ {
			if cm[row*256+c] != byte(c) {
				t.Fatalf("fullbright %d moves in row %d", c, row)
			}
		}
		for c := 0; c < 224; c++ {
			if cm[row*256+c] >= 224 {
				t.Fatalf("lit colour %d becomes fullbright in row %d", c, row)
			}
		}
	}
	for c := 0; c < 256; c++ {
		if cm[32*256+c] != byte(c) {
			t.Fatalf("row 32 is not the identity at %d", c)
		}
	}
	// Darker rows are darker, brighter rows brighter (mean lightness).
	mean := func(row int) float64 {
		s := 0.0
		for c := 1; c < 224; c++ {
			s += lightness(pal, int(cm[row*256+c]))
		}
		return s
	}
	if !(mean(0) > mean(16) && mean(16) > mean(32) && mean(32) > mean(48) && mean(48) > mean(63)) {
		t.Errorf("rows are not ordered by lightness: %v %v %v %v %v", mean(0), mean(16), mean(32), mean(48), mean(63))
	}
	if mean(63) > 224*0.1 {
		t.Errorf("row 63 is not nearly black: %.2f", mean(63))
	}
}

func put32(b []byte, off, v int) { binary.LittleEndian.PutUint32(b[off:], uint32(int32(v))) }

func newRemapForTest() *Remap { return NewRemap(noise(41, 768), OwnPalette()) }

func TestReindexPic(t *testing.T) {
	m := newRemapForTest()
	b := make([]byte, 8+6)
	put32(b, 0, 3)
	put32(b, 4, 2)
	copy(b[8:], []byte{0, 5, 100, 230, 255, 17})
	if err := ReindexPic(b, m); err != nil {
		t.Fatal(err)
	}
	if b[12] != 255 || b[8] != m.Table[0] || b[10] != m.Table[100] {
		t.Errorf("pixels %v", b[8:])
	}
	for _, bad := range [][]byte{b[:5], b[:12], append(bytes.Clone(b), 1)} {
		if err := ReindexPic(bad, newRemapForTest()); err == nil {
			t.Errorf("accepted %d bytes", len(bad))
		}
	}
	neg := bytes.Clone(b)
	put32(neg, 0, -1)
	if err := ReindexPic(neg, newRemapForTest()); err == nil {
		t.Error("accepted negative width")
	}
}

func miptexLump(w, h int) []byte {
	// header: nummiptex=1, offset[0]=8; texture at 8: name, w, h, 4 offsets
	tex := make([]byte, 40)
	copy(tex, "tex")
	put32(tex, 16, w)
	put32(tex, 20, h)
	pix := 0
	off := 40
	for l := 0; l < 4; l++ {
		put32(tex, 24+4*l, off+pix)
		pix += (w >> l) * (h >> l)
	}
	for i := 0; i < pix; i++ {
		tex = append(tex, byte(i*7))
	}
	lump := make([]byte, 8)
	put32(lump, 0, 1)
	put32(lump, 4, 8)
	return append(lump, tex...)
}

func TestReindexBSP(t *testing.T) {
	m := newRemapForTest()
	lump := miptexLump(16, 16)
	bsp := fakeBSP("{}", nil)
	// Put the texture lump (lump 2) at the end.
	for len(bsp)%4 != 0 {
		bsp = append(bsp, 0)
	}
	put32(bsp, 4+2*8, len(bsp))
	put32(bsp, 8+2*8, len(lump))
	bsp = append(bsp, lump...)
	orig := bytes.Clone(bsp)
	if err := ReindexBSP(bsp, m); err != nil {
		t.Fatal(err)
	}
	if len(bsp) != len(orig) {
		t.Fatal("size changed")
	}
	tex := bsp[len(bsp)-len(lump)+8:]
	otex := orig[len(orig)-len(lump)+8:]
	n := 16*16 + 8*8 + 4*4 + 2*2
	for i := 0; i < n; i++ {
		if tex[40+i] != m.Table[otex[40+i]] {
			t.Fatalf("texel %d: %d -> %d", i, otex[40+i], tex[40+i])
		}
	}
	if m.Count[0] == 0 && m.Stats().Texels == 0 {
		t.Error("nothing counted")
	}
	if got := m.Stats().Texels; got == 0 {
		t.Error("no texels")
	}
	// Two textures sharing the same pixels would be remapped twice: refused.
	two := miptexLump(16, 16)
	put32(two, 0, 2)
	two = append(two[:8:8], two[8:]...) // keep layout
	shared := bytes.Clone(two)
	shared = append(shared[:8], shared[8:]...)
	// Rebuild: lump with 2 offsets both pointing at the same texture.
	l2 := make([]byte, 12)
	put32(l2, 0, 2)
	put32(l2, 4, 12)
	put32(l2, 8, 12)
	l2 = append(l2, miptexLump(16, 16)[8:]...)
	b2 := fakeBSP("{}", nil)
	for len(b2)%4 != 0 {
		b2 = append(b2, 0)
	}
	put32(b2, 4+2*8, len(b2))
	put32(b2, 8+2*8, len(l2))
	b2 = append(b2, l2...)
	if _, err := ReindexEntry("maps/x.bsp", b2, newRemapForTest(), OwnPalette()); err == nil {
		t.Error("shared texels accepted")
	}
	// Bad inputs.
	bad := bytes.Clone(orig)
	put32(bad, 8+2*8, 1<<28)
	if err := ReindexBSP(bad, newRemapForTest()); err == nil {
		t.Error("texture lump out of range accepted")
	}
	if err := ReindexBSP(orig[:30], newRemapForTest()); err == nil {
		t.Error("short bsp accepted")
	}
	wrongMip := bytes.Clone(orig)
	put32(wrongMip, len(wrongMip)-len(lump)+8+24, 1<<20)
	if err := ReindexBSP(wrongMip, newRemapForTest()); err == nil {
		t.Error("mip offset out of range accepted")
	}
}

func TestReindexMDL(t *testing.T) {
	m := newRemapForTest()
	b := make([]byte, 84)
	copy(b, "IDPO")
	put32(b, 4, 6)
	put32(b, 48, 2) // skins
	put32(b, 52, 4)
	put32(b, 56, 4)
	// skin 0: single
	b = append(b, 0, 0, 0, 0)
	b = append(b, bytes.Repeat([]byte{100}, 16)...)
	// skin 1: group of 2
	b = append(b, 1, 0, 0, 0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	b = append(b, bytes.Repeat([]byte{255}, 16)...)
	b = append(b, bytes.Repeat([]byte{20}, 16)...)
	if err := ReindexMDL(b, m); err != nil {
		t.Fatal(err)
	}
	if b[88] != m.Table[100] || b[len(b)-32] != 255 || b[len(b)-1] != m.Table[20] {
		t.Error("skins not remapped")
	}
	if m.Table[20] < 16 || m.Table[20] > 31 {
		t.Error("top-range skin colour left its range")
	}
	if err := ReindexMDL(b[:100], newRemapForTest()); err == nil {
		t.Error("truncated mdl accepted")
	}
	bad := bytes.Clone(b)
	bad[0] = 'X'
	if err := ReindexMDL(bad, newRemapForTest()); err == nil {
		t.Error("bad magic accepted")
	}
}

func TestReindexSPR(t *testing.T) {
	m := newRemapForTest()
	b := make([]byte, 36)
	copy(b, "IDSP")
	put32(b, 4, 1)
	put32(b, 24, 2) // frames
	// frame 0 single 2x2
	b = append(b, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2, 0, 0, 0, 2, 0, 0, 0, 255, 100, 100, 255)
	// frame 1 group of 1, 1x1
	b = append(b, 1, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0)
	b = append(b, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 7)
	if err := ReindexSPR(b, m); err != nil {
		t.Fatal(err)
	}
	if b[36+20] != 255 || b[36+21] != m.Table[100] || b[len(b)-1] != m.Table[7] {
		t.Errorf("sprite pixels %v", b[56:])
	}
	if err := ReindexSPR(b[:50], newRemapForTest()); err == nil {
		t.Error("truncated sprite accepted")
	}
}

func TestReindexWAD(t *testing.T) {
	m := newRemapForTest()
	pic := make([]byte, 8+4)
	put32(pic, 0, 2)
	put32(pic, 4, 2)
	copy(pic[8:], []byte{1, 2, 255, 4})
	conchars := bytes.Repeat([]byte{9}, 128*128)
	pal := make([]byte, 768)
	var data []byte
	data = append(data, "WAD2"...)
	data = append(data, 3, 0, 0, 0, 0, 0, 0, 0)
	type lump struct {
		name string
		typ  byte
		d    []byte
	}
	lumps := []lump{{"pic", wadQPic, pic}, {"conchars", wadMipTex, conchars}, {"palette", wadPalette, pal}}
	var info []byte
	for _, l := range lumps {
		e := make([]byte, 32)
		put32(e, 0, len(data))
		put32(e, 4, len(l.d))
		put32(e, 8, len(l.d))
		e[12] = l.typ
		copy(e[16:], l.name)
		info = append(info, e...)
		data = append(data, l.d...)
	}
	put32(data, 8, len(data))
	data = append(data, info...)
	orig := bytes.Clone(data)
	own := OwnPalette()
	if err := ReindexWAD(data, m, own); err != nil {
		t.Fatal(err)
	}
	if data[12+8+2] != 255 || data[12+8] != m.Table[1] {
		t.Error("qpic not remapped")
	}
	if data[12+12+0] != m.Table[9] {
		t.Error("conchars not remapped")
	}
	if !bytes.Contains(data, own) || bytes.Equal(data, orig) {
		t.Error("palette lump not replaced")
	}
	bad := bytes.Clone(orig)
	bad[len(bad)-32+12] = 99 // unknown type
	if err := ReindexWAD(bad, newRemapForTest(), own); err == nil {
		t.Error("unknown lump type accepted")
	}
	bad = bytes.Clone(orig)
	bad[len(bad)-32+13] = 1 // compressed
	if err := ReindexWAD(bad, newRemapForTest(), own); err == nil {
		t.Error("compressed lump accepted")
	}
	if err := ReindexWAD(orig[:10], newRemapForTest(), own); err == nil {
		t.Error("short wad accepted")
	}
}

func TestReindexEntryRouting(t *testing.T) {
	m := newRemapForTest()
	for _, n := range []string{"progs.dat", "sound/x.wav", "gfx/palette.lmp", "gfx/colormap.lmp", "quake.rc"} {
		if did, err := ReindexEntry(n, []byte("x"), m, OwnPalette()); did || err != nil {
			t.Errorf("%s: did=%v err=%v", n, did, err)
		}
	}
	if _, err := ReindexEntry("gfx/pause.lmp", []byte("x"), m, OwnPalette()); err == nil {
		t.Error("junk pic accepted")
	}
	if _, err := ReindexEntry("progs/x.mdl", []byte("x"), m, OwnPalette()); err == nil {
		t.Error("junk mdl accepted")
	}
	if _, err := ReindexEntry("progs/x.spr", []byte("x"), m, OwnPalette()); err == nil {
		t.Error("junk spr accepted")
	}
	if _, err := ReindexEntry("gfx.wad", []byte("x"), m, OwnPalette()); err == nil {
		t.Error("junk wad accepted")
	}
}

func TestOKLabRoundTrip(t *testing.T) {
	for _, c := range [][3]uint8{{0, 0, 0}, {255, 255, 255}, {12, 200, 90}, {250, 3, 77}} {
		r, g, b := LabToRGB(RGBToLab(c[0], c[1], c[2]))
		if r != c[0] || g != c[1] || b != c[2] {
			t.Errorf("%v -> %d %d %d", c, r, g, b)
		}
	}
	// Out-of-gamut colours are brought in by reducing chroma.
	r, g, b := LabToRGB(Lab{L: 0.5, A: 0.4, B: 0.3})
	_ = r + g + b
	if d := DeltaE(Lab{0.5, 0, 0}, Lab{0.5, 0, 0}); d != 0 {
		t.Error("DeltaE of equal colours")
	}
}

func TestStructureReport(t *testing.T) {
	pal := OwnPalette()
	s, err := StructureReport([]File{{"gfx/palette.lmp", pal}, {"gfx/colormap.lmp", Colormap(pal)}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"block  0", "block 15", "dark->light", "light->dark", "last byte = 32", "row 32: identity cells 224 of 224"} {
		if !bytes.Contains([]byte(s), []byte(want)) {
			t.Errorf("report lacks %q:\n%s", want, s)
		}
	}
	if _, err := StructureReport(nil); err == nil {
		t.Error("empty reference accepted")
	}
}
