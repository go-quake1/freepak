package freepak

import "math"

// Lab is a colour in OKLab (Ottosson, 2020, https://bottosson.github.io/posts/oklab/).
type Lab struct{ L, A, B float64 }

func srgbToLinear(c float64) float64 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow(float64((c+0.055)/1.055), 2.4)
}

func linearToSRGB(c float64) float64 {
	if c <= 0.0031308 {
		return float64(12.92 * c)
	}
	return float64(1.055*math.Pow(c, 1/2.4)) - 0.055
}

// RGBToLab converts 8-bit sRGB to OKLab. The explicit float64 conversions stop
// the compiler from fusing multiply-adds, so every architecture rounds alike.
func RGBToLab(r, g, b uint8) Lab {
	lr, lg, lb := srgbToLinear(float64(r)/255), srgbToLinear(float64(g)/255), srgbToLinear(float64(b)/255)
	l := math.Cbrt(float64(float64(0.4122214708*lr)+float64(0.5363325363*lg)) + float64(0.0514459929*lb))
	m := math.Cbrt(float64(float64(0.2119034982*lr)+float64(0.6806995451*lg)) + float64(0.1073969566*lb))
	s := math.Cbrt(float64(float64(0.0883024619*lr)+float64(0.2817188376*lg)) + float64(0.6299787005*lb))
	return Lab{
		L: float64(float64(0.2104542553*l)+float64(0.7936177850*m)) - float64(0.0040720468*s),
		A: float64(float64(1.9779984951*l)-float64(2.4285922050*m)) + float64(0.4505937099*s),
		B: float64(float64(0.0259040371*l)+float64(0.7827717662*m)) - float64(0.8086757660*s),
	}
}

// linearRGB returns the (possibly out of gamut) linear sRGB of a Lab colour.
func (c Lab) linearRGB() (r, g, b float64) {
	l := float64(c.L+float64(0.3963377774*c.A)) + float64(0.2158037573*c.B)
	m := float64(c.L-float64(0.1055613458*c.A)) - float64(0.0638541728*c.B)
	s := float64(c.L-float64(0.0894841775*c.A)) - float64(1.2914855480*c.B)
	l, m, s = l*l*l, m*m*m, s*s*s
	r = float64(float64(4.0767416621*l)-float64(3.3077115913*m)) + float64(0.2309699292*s)
	g = float64(float64(-1.2684380046*l)+float64(2.6097574011*m)) - float64(0.3413193965*s)
	b = float64(float64(-0.0041960863*l)-float64(0.7034186147*m)) + float64(1.7076147010*s)
	return
}

func inGamut(r, g, b float64) bool {
	const e = 1e-9
	return r >= -e && r <= 1+e && g >= -e && g <= 1+e && b >= -e && b <= 1+e
}

func to8(c float64) uint8 {
	c = math.Min(1, math.Max(0, c))
	return uint8(math.Floor(float64(255*linearToSRGB(c)) + 0.5))
}

// LabToRGB converts to 8-bit sRGB, reducing chroma (by bisection, 40 steps)
// until the colour is inside the sRGB gamut.
func LabToRGB(c Lab) (uint8, uint8, uint8) {
	if r, g, b := c.linearRGB(); !inGamut(r, g, b) {
		lo, hi := 0.0, 1.0
		for i := 0; i < 40; i++ {
			mid := (lo + hi) / 2
			t := Lab{c.L, c.A * mid, c.B * mid}
			if r, g, b := t.linearRGB(); inGamut(r, g, b) {
				lo = mid
			} else {
				hi = mid
			}
		}
		c = Lab{c.L, c.A * lo, c.B * lo}
	}
	r, g, b := c.linearRGB()
	return to8(r), to8(g), to8(b)
}

// DeltaE is the Euclidean distance in OKLab, times 100 (so that 1 is about a
// just-noticeable difference).
func DeltaE(a, b Lab) float64 {
	dl, da, db := a.L-b.L, a.A-b.A, a.B-b.B
	return 100 * math.Sqrt(float64(float64(dl*dl)+float64(da*da))+float64(db*db))
}
