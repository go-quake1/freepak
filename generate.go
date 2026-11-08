package freepak

import (
	"encoding/binary"
	"strings"
)

// End1Bin returns the contents of end1.bin: an 80x25 text-mode screen, 4000 bytes of
// (character, attribute) pairs in code page 437, written for this project.
func End1Bin() []byte {
	const cols, rows = 80, 25
	screen := make([]byte, cols*rows*2)
	put := func(x, y int, ch, attr byte) {
		i := (y*cols + x) * 2
		screen[i], screen[i+1] = ch, attr
	}
	// Background: dark blue blanks.
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			put(x, y, ' ', 0x17)
		}
	}
	// Double-line frame, bright yellow on blue (CP437 0xC9 0xCD 0xBB 0xBA 0xC8 0xBC).
	const frame = 0x1E
	for x := 1; x < cols-1; x++ {
		put(x, 0, 0xCD, frame)
		put(x, rows-1, 0xCD, frame)
	}
	for y := 1; y < rows-1; y++ {
		put(0, y, 0xBA, frame)
		put(cols-1, y, 0xBA, frame)
	}
	put(0, 0, 0xC9, frame)
	put(cols-1, 0, 0xBB, frame)
	put(0, rows-1, 0xC8, frame)
	put(cols-1, rows-1, 0xBC, frame)

	centre := func(y int, s string, attr byte) {
		x := (cols - len(s)) / 2
		for i := 0; i < len(s); i++ {
			put(x+i, y, s[i], attr)
		}
	}
	centre(3, strings.Join(strings.Split("FREEPAK", ""), " "), 0x4F)
	centre(5, "The free Quake data set", 0x1F)
	centre(8, "Thank you for playing.", 0x1B)
	centre(11, "Maps, models, textures, sounds: LibreQuake (BSD-3-Clause)", 0x17)
	centre(12, "Game code: LibreProgsDeluxe (GPL-2.0-or-later)", 0x17)
	centre(13, "Assembled by go-quake1/freepak (BSD-3-Clause)", 0x17)
	centre(16, "github.com/go-quake1/freepak", 0x1E)
	centre(19, "Quake is a trademark of id Software; this set contains none of its data.", 0x13)
	return screen
}

const (
	wavRate = 11025
	wavLen  = wavRate * 9 / 10
)

// sine is a parabolic sine approximation in integers: phase is a full turn
// over 2^32, the result is in [-32768, 32768]. Integers only, so the output
// is the same on every architecture (no fused multiply-add).
func sine(phase uint32) int32 {
	x := int32(phase >> 16)
	sign := int32(1)
	if x >= 32768 {
		sign, x = -1, x-32768
	}
	return sign * (x * (32768 - x) >> 13)
}

// Slimbrn2Wav synthesises sound/player/slimbrn2.wav: a short burbling gurgle,
// three rising bubbles over low-passed noise, 11025 Hz, 8-bit unsigned mono.
// It is deterministic integer code written for this project; nothing is
// sampled from any recording.
func Slimbrn2Wav() []byte {
	acc := make([]int32, wavLen)
	type bubble struct{ start, length, f0, f1, amp int }
	for _, b := range []bubble{
		{400, 2200, 180, 520, 9000},
		{3300, 2600, 140, 460, 10000},
		{6600, 3000, 110, 380, 8000},
	} {
		var phase uint32
		for n := 0; n < b.length && b.start+n < wavLen; n++ {
			f := b.f0 + (b.f1-b.f0)*n/b.length
			phase += uint32(uint64(f) << 32 / wavRate)
			e := (b.length - n) * 1000 / b.length
			amp := b.amp * e / 1000 * e / 1000
			acc[b.start+n] += sine(phase) * int32(amp) >> 15
		}
	}
	// Burble: xorshift noise, one-pole low-pass, slow tremolo, fading out.
	seed := uint32(0x9E3779B9)
	var lp int32
	var trem uint32
	for n := 0; n < wavLen; n++ {
		seed ^= seed << 13
		seed ^= seed >> 17
		seed ^= seed << 5
		noise := int32(int16(seed >> 16))
		lp += (noise - lp) >> 3
		trem += uint32(uint64(11) << 32 / wavRate)
		depth := (sine(trem) + 32768) / 2 // 0..32768
		fade := int32(wavLen-n) * 1000 / wavLen
		acc[n] += lp * depth >> 15 * fade / 1000 / 4
	}
	out := make([]byte, 44+wavLen)
	copy(out, "RIFF")
	binary.LittleEndian.PutUint32(out[4:], uint32(36+wavLen))
	copy(out[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(out[16:], 16)
	binary.LittleEndian.PutUint16(out[20:], 1) // PCM
	binary.LittleEndian.PutUint16(out[22:], 1) // mono
	binary.LittleEndian.PutUint32(out[24:], wavRate)
	binary.LittleEndian.PutUint32(out[28:], wavRate) // byte rate
	binary.LittleEndian.PutUint16(out[32:], 1)       // block align
	binary.LittleEndian.PutUint16(out[34:], 8)       // bits
	copy(out[36:], "data")
	binary.LittleEndian.PutUint32(out[40:], wavLen)
	for n, v := range acc {
		s := 128 + v>>7
		if s < 0 {
			s = 0
		}
		if s > 255 {
			s = 255
		}
		out[44+n] = byte(s)
	}
	return out
}
