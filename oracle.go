package freepak

import (
	"bytes"
	"fmt"
	"math"
)

// StructureReport describes, from a reference pak's palette and colormap, only
// the STRUCTURE the engine depends on: per 16-entry block the mean lightness,
// mean chroma, coarse hue and which end is lighter, and facts about the
// colormap. It prints no RGB value. It is a measurement oracle for choosing the
// parameters of [OwnPalette] by hand, and is not used by the build.
func StructureReport(ref []File) (string, error) {
	var pal, cm []byte
	for _, f := range ref {
		switch f.Name {
		case "gfx/palette.lmp":
			pal = f.Data
		case "gfx/colormap.lmp":
			cm = f.Data
		}
	}
	if len(pal) != 768 || len(cm) < 16384 {
		return "", fmt.Errorf("reference lacks palette (%d bytes) or colormap (%d bytes)", len(pal), len(cm))
	}
	var b bytes.Buffer
	for blk := 0; blk < 16; blk++ {
		var sl, sc, sa, sb float64
		var first, last Lab
		for i := 0; i < 16; i++ {
			k := blk*16 + i
			lab := RGBToLab(pal[k*3], pal[k*3+1], pal[k*3+2])
			if i == 0 {
				first = lab
			}
			last = lab
			sl += lab.L
			sc += math.Hypot(lab.A, lab.B)
			sa += lab.A
			sb += lab.B
		}
		hue := math.Atan2(sb, sa) * 180 / math.Pi
		if hue < 0 {
			hue += 360
		}
		dir := "dark->light"
		if first.L > last.L {
			dir = "light->dark"
		}
		fmt.Fprintf(&b, "block %2d (%3d-%3d): L %.2f C %.3f hue %3.0f  %s\n", blk, blk*16, blk*16+15, sl/16, sc/16, hue, dir)
	}
	id := true
	for c := 0; c < 256; c++ {
		if cm[c] != byte(c) {
			id = false
		}
	}
	fixed := 0
	for c := 0; c < 256; c++ {
		same := true
		for row := 0; row < 64; row++ {
			if cm[row*256+c] != byte(c) {
				same = false
			}
		}
		if same {
			fixed++
		}
	}
	// How a row darkens: mean ratio of the sRGB channel sums (result / source)
	// over the lit colours with a sum above 150, and the number of identity cells.
	for _, row := range []int{0, 1, 8, 16, 32, 48, 56, 63} {
		var num, den float64
		same := 0
		for c := 0; c < 224; c++ {
			d := int(cm[row*256+c])
			if d == c {
				same++
			}
			s := int(pal[c*3]) + int(pal[c*3+1]) + int(pal[c*3+2])
			if s > 150 {
				num += float64(int(pal[d*3]) + int(pal[d*3+1]) + int(pal[d*3+2]))
				den += float64(s)
			}
		}
		fmt.Fprintf(&b, "colormap row %2d: identity cells %3d of 224, mean sRGB-sum ratio %.3f\n", row, same, num/den)
	}
	fmt.Fprintf(&b, "colormap: %d bytes, row 0 is identity: %v, columns identical in all 64 rows: %d\n", len(cm), id, fixed)
	fmt.Fprintf(&b, "colormap: int at byte 8192 = %d, last byte = %d\n", int32(uint32(cm[8192])|uint32(cm[8193])<<8|uint32(cm[8194])<<16|uint32(cm[8195])<<24), cm[len(cm)-1])
	return b.String(), nil
}
