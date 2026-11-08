package freepak

import (
	"errors"
	"math"
)

// The engine gives palette indices a meaning; OwnPalette keeps the meaning and
// none of the values of any other palette. Sources, all in go-quake1/engine
// (which ports tyrquake):
//
//	255            transparent in pics, sprites and models (render.TransparentIndex)
//	0              transparent in the front sky layer (render.SkyTransparentIndex), and in conchars
//	224..255       fullbright: the colormap maps them to themselves in every row
//	16..31         player "top" colour range (TOP_RANGE 16), 16 entries, dark to light
//	96..111        player "bottom" colour range (BOTTOM_RANGE 96); also the fire ramps
//	               Ramp1 = 0x6f,0x6d,0x6b,0x69,0x67,0x65,0x63,0x61 and Ramp2 = 0x6f,0x6e,..,0x66
//	               and the first two entries of Ramp3 (0x6d, 0x6b)
//	3..6           Ramp3's smoke tail (0x6d,0x6b,6,5,4,3): dark greys
//	7..14          white-grey sparks (teleport splashes, render/particle.go)
//	52, 54         green blood band, 67..70 red blood band, 152..155 magenta band,
//	224..231       explosion sparks, 230 and 232 orange band
//
// A shirt or pants colour c replaces a 16-entry range by the block c*16..c*16+15
// (a block >= 128 is applied in reverse order), so every 16-entry block is a
// ramp of one hue, and blocks 8..13 run light to dark. The fixed indices above
// then keep their colour family: red at 64..79, fire at 96..111 and 224..239,
// greys at 0..15, green at 48..63, magenta at 144..159.
type ramp struct {
	h0, h1 float64 // OKLab hue angle in degrees at the first and last entry
	c0, c1 float64 // chroma at the first and last entry
	l0, l1 float64 // lightness at the first and last entry
}

// ramps are authored here, by hue family, lightness range and chroma range; no
// value is taken from any other palette. Index order inside a block follows the
// engine: dark to light for blocks 0..7 and 14, light to dark for 8..13 and 15.
var ramps = [16]ramp{
	0:  {0, 0, 0, 0, 0.00, 1.00},           // greys: black .. white
	1:  {70, 85, 0.02, 0.08, 0.15, 0.80},   // brown (top colour range)
	2:  {275, 265, 0.03, 0.11, 0.15, 0.82}, // blue
	3:  {125, 110, 0.03, 0.13, 0.15, 0.78}, // olive green
	4:  {28, 30, 0.05, 0.20, 0.14, 0.70},   // red (blood)
	5:  {95, 82, 0.03, 0.12, 0.14, 0.80},   // olive brown
	6:  {40, 75, 0.06, 0.17, 0.16, 0.92},   // orange fire (bottom colour range)
	7:  {50, 60, 0.03, 0.09, 0.20, 0.86},   // peach
	8:  {355, 355, 0.04, 0.10, 0.85, 0.15}, // rose
	9:  {350, 345, 0.05, 0.14, 0.88, 0.18}, // magenta
	10: {55, 55, 0.03, 0.07, 0.84, 0.20},   // tan
	11: {150, 150, 0.04, 0.10, 0.82, 0.18}, // green
	12: {100, 90, 0.10, 0.13, 0.92, 0.28},  // yellow
	13: {268, 268, 0.08, 0.20, 0.82, 0.16}, // violet blue
	14: {30, 75, 0.20, 0.14, 0.40, 0.97},   // fullbright fire, red to yellow
	15: {95, 55, 0.04, 0.15, 1.00, 0.55},   // fullbright yellows, light to dark
}

// OwnPalette returns the 768-byte palette generated from ramps: 16 steps per
// block, linear in OKLab lightness, chroma and hue, chroma reduced where the
// colour leaves the sRGB gamut.
func OwnPalette() []byte {
	pal := make([]byte, 768)
	for blk, r := range ramps {
		for i := 0; i < 16; i++ {
			t := float64(i) / 15
			h := float64(r.h0+float64((r.h1-r.h0)*t)) * math.Pi / 180
			c := float64(r.c0 + float64((r.c1-r.c0)*t))
			lab := Lab{L: float64(r.l0 + float64((r.l1-r.l0)*t)), A: float64(c * math.Cos(h)), B: float64(c * math.Sin(h))}
			R, G, B := LabToRGB(lab)
			k := (blk*16 + i) * 3
			pal[k], pal[k+1], pal[k+2] = R, G, B
		}
	}
	return pal
}

// Index classes of the engine (see the comment above).
const (
	transparentIndex = 255
	fullbrightFirst  = 224
)

func inTranslationBlock(i int) bool { return (i >= 16 && i < 32) || (i >= 96 && i < 112) }

// candidates lists the new indices an old index may take: its own class, so
// that every meaning above survives.
func candidates(old int) (lo, hi int, skipTranslation bool) {
	switch {
	case old == transparentIndex:
		return 255, 255, false
	case old >= fullbrightFirst:
		return fullbrightFirst, 254, false
	case old >= 16 && old < 32:
		return 16, 31, false
	case old >= 96 && old < 112:
		return 96, 111, false
	}
	return 0, fullbrightFirst - 1, true
}

// Remap maps the indices of an old palette to the nearest indices of a new one.
type Remap struct {
	Table [256]byte
	// DeltaE[i] is the OKLab distance (times 100) between old colour i and the
	// new colour it maps to.
	DeltaE [256]float64
	// Count[i] is the number of texels that had old index i, filled by the
	// reindexing functions.
	Count [256]uint64
	// Err is set by Apply when texels were referenced twice.
	Err  error
	seen map[*byte]bool
}

// NewRemap builds the table. Each old index goes to the nearest new colour in
// OKLab among the new indices of its class: 255 stays 255, 224..254 stay
// fullbright, 16..31 and 96..111 stay in their block (so player colours keep
// working), and the rest map to the other non-fullbright entries. Ties go to
// the same index, then to the lowest.
func NewRemap(oldPal, newPal []byte) *Remap {
	var newLab, oldLab [256]Lab
	for i := 0; i < 256; i++ {
		newLab[i] = RGBToLab(newPal[i*3], newPal[i*3+1], newPal[i*3+2])
		oldLab[i] = RGBToLab(oldPal[i*3], oldPal[i*3+1], oldPal[i*3+2])
	}
	m := new(Remap)
	for old := 0; old < 256; old++ {
		lo, hi, skip := candidates(old)
		best, bd := -1, math.Inf(1)
		for n := lo; n <= hi; n++ {
			if skip && inTranslationBlock(n) {
				continue
			}
			d := DeltaE(oldLab[old], newLab[n])
			if d < bd || (d == bd && n == old) {
				best, bd = n, d
			}
		}
		m.Table[old] = byte(best)
		m.DeltaE[old] = bd
	}
	return m
}

// Apply rewrites texels in place, counting them.
func (m *Remap) Apply(texels []byte) {
	if len(texels) == 0 {
		return
	}
	// Two references to the same texels would be remapped twice: refuse.
	if m.seen == nil {
		m.seen = map[*byte]bool{}
	}
	if m.seen[&texels[0]] {
		m.Err = errors.New("the same texels are referenced twice")
	}
	m.seen[&texels[0]] = true
	for i, b := range texels {
		m.Count[b]++
		texels[i] = m.Table[b]
	}
}

// Quantisation summarises the colour error of a reindexing over all texels
// counted by Apply, skipping the transparent index (whose colour is never shown).
type Quantisation struct {
	Texels uint64
	Mean   float64
	Max    float64
	MaxIdx int
}

// Stats returns the texel-weighted mean and the maximum OKLab distance.
func (m *Remap) Stats() Quantisation {
	var q Quantisation
	var sum float64
	for i := 0; i < 255; i++ {
		if m.Count[i] == 0 {
			continue
		}
		q.Texels += m.Count[i]
		sum += float64(m.Count[i]) * m.DeltaE[i]
		if m.DeltaE[i] > q.Max {
			q.Max, q.MaxIdx = m.DeltaE[i], i
		}
	}
	if q.Texels > 0 {
		q.Mean = sum / float64(q.Texels)
	}
	return q
}

// Colormap returns the 16385-byte gfx/colormap.lmp for pal: 64 light rows of
// 256 entries and a trailing byte holding the number of fullbright colours.
//
// Row r scales every lit colour (0..223) by (64-r)/32 in sRGB (row 32 is the
// identity, row 0 doubles, row 63 is nearly black), clips to 255 and takes the
// nearest lit palette entry in OKLab (ties go to the source index, so row 32 is
// exactly the identity). The fullbright colours 224..255 map to themselves in
// every row. The trailing byte is 32, the size of that range.
func Colormap(pal []byte) []byte {
	var lab [224]Lab
	for i := range lab {
		lab[i] = RGBToLab(pal[i*3], pal[i*3+1], pal[i*3+2])
	}
	out := make([]byte, 64*256+1)
	for row := 0; row < 64; row++ {
		for c := 0; c < 256; c++ {
			if c >= fullbrightFirst {
				out[row*256+c] = byte(c)
				continue
			}
			sc := func(v byte) uint8 {
				s := int(v) * (64 - row) / 32
				if s > 255 {
					s = 255
				}
				return uint8(s)
			}
			want := RGBToLab(sc(pal[c*3]), sc(pal[c*3+1]), sc(pal[c*3+2]))
			best, bd := 0, math.Inf(1)
			for n := 0; n < 224; n++ {
				d := DeltaE(want, lab[n])
				if d < bd || (d == bd && n == c) {
					best, bd = n, d
				}
			}
			out[row*256+c] = byte(best)
		}
	}
	out[64*256] = 256 - fullbrightFirst
	return out
}
