package freepak

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/go-quake1/engine/sound"
)

// wav8 builds a mono 8-bit PCM WAV of n samples, with the given extra chunks
// after the data chunk.
func wav8(n int, extra ...[]byte) []byte {
	chunk := func(id string, body []byte) []byte {
		b := append([]byte(id), 0, 0, 0, 0)
		binary.LittleEndian.PutUint32(b[4:], uint32(len(body)))
		b = append(b, body...)
		if len(body)%2 == 1 {
			b = append(b, 0)
		}
		return b
	}
	f := make([]byte, 16)
	binary.LittleEndian.PutUint16(f[0:], 1)
	binary.LittleEndian.PutUint16(f[2:], 1)
	binary.LittleEndian.PutUint32(f[4:], 11025)
	binary.LittleEndian.PutUint16(f[14:], 8)
	body := []byte("WAVE")
	body = append(body, chunk("fmt ", f)...)
	body = append(body, chunk("data", noise(5, n))...)
	for _, e := range extra {
		body = append(body, e...)
	}
	out := append([]byte("RIFF"), 0, 0, 0, 0)
	binary.LittleEndian.PutUint32(out[4:], uint32(len(body)))
	return append(out, body...)
}

// EnsureLoop adds the cue chunk the engine reads as a loop start of 0, changes
// nothing else, keeps the RIFF size right, pads an odd length, and leaves a
// sample that already has a loop point exactly as it is.
func TestEnsureLoop(t *testing.T) {
	for _, n := range []int{1000, 1001} { // even and odd data lengths
		in := wav8(n, []byte("LIST\x04\x00\x00\x00INFO"))
		if has, err := HasCue(in); err != nil || has {
			t.Fatalf("a sample with no cue chunk: has=%v err=%v", has, err)
		}
		out, added, err := EnsureLoop(in)
		if err != nil || !added {
			t.Fatalf("n=%d: added=%v err=%v", n, added, err)
		}
		if got := binary.LittleEndian.Uint32(out[4:]); int(got) != len(out)-8 {
			t.Errorf("n=%d: RIFF size %d, file is %d bytes", n, got, len(out))
		}
		if !bytes.Contains(out, noise(5, n)) {
			t.Errorf("n=%d: the sample data changed", n)
		}
		s, err := sound.LoadWav("x", out) // the engine's reader (GetWavinfo's port)
		if err != nil || s.LoopStart != 0 || s.NumSamples != n {
			t.Fatalf("n=%d: engine reads %+v, %v; want loopstart 0 over %d samples", n, s, err, n)
		}
		again, added, err := EnsureLoop(out)
		if err != nil || added || !bytes.Equal(again, out) {
			t.Errorf("n=%d: a second EnsureLoop changed the file (added=%v)", n, added)
		}
	}
	// A cue point already there is kept (wind2.wav's loop start is LibreQuake's).
	cue := make([]byte, 8+28)
	copy(cue, "cue ")
	binary.LittleEndian.PutUint32(cue[4:], 28)
	binary.LittleEndian.PutUint32(cue[8:], 1)
	binary.LittleEndian.PutUint32(cue[8+24:], 321)
	in := wav8(1000, cue)
	out, added, err := EnsureLoop(in)
	if err != nil || added || !bytes.Equal(out, in) {
		t.Fatalf("a sample with a cue chunk: added=%v err=%v", added, err)
	}
	if s, _ := sound.LoadWav("x", out); s.LoopStart != 321 {
		t.Errorf("loop start %d, want LibreQuake's 321", s.LoopStart)
	}
	for _, bad := range [][]byte{nil, []byte("RIFF"), bytes.Repeat([]byte{1}, 40)} {
		if _, _, err := EnsureLoop(bad); err == nil {
			t.Errorf("%d bytes of not a WAV accepted", len(bad))
		}
	}
}

// Every sample the game must loop carries a cue point in the built pak, and the
// engine reads it as a loop start. LibreQuake's own water1.wav has none (the
// planted positive: the test sees what it is for), so the build adds it.
func TestLoopingSoundsHaveCuePoints(t *testing.T) {
	lq, err := OpenLibreQuake(input(t, "FREEPAK_LQ"))
	if err != nil {
		t.Fatal(err)
	}
	raw, ok := lq.Lookup("sound/ambience/water1.wav")
	if !ok {
		t.Fatal("librequake has no ambience/water1.wav")
	}
	if has, _ := HasCue(raw); has {
		t.Fatal("LibreQuake's water1.wav now has a cue point: the build's EnsureLoop is no longer needed here; update LoopingSounds' comment")
	}
	files, entries, _, err := Assemble(lq)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]File{}
	for _, f := range files {
		byName[f.Name] = f
	}
	origin := map[string]string{}
	for _, e := range entries {
		origin[e.Path] = e.Origin
	}
	for _, name := range LoopingSounds {
		f, ok := byName[name]
		if !ok {
			t.Errorf("%s is not in the pak", name)
			continue
		}
		has, err := HasCue(f.Data)
		if err != nil || !has {
			t.Errorf("%s has no cue point (has=%v err=%v): the game would play it once", name, has, err)
			continue
		}
		s, err := sound.LoadWav(name, f.Data)
		if err != nil || s.LoopStart < 0 || s.LoopStart >= s.NumSamples {
			t.Errorf("%s: the engine reads %+v, %v; want a loop start inside the sample", name, s, err)
		}
	}
	if o := origin["sound/ambience/water1.wav"]; o != "librequake:sound/ambience/water1.wav+cue" {
		t.Errorf("water1.wav's origin is %q: the MANIFEST must say the cue point was added", o)
	}
	if o := origin["sound/ambience/wind2.wav"]; o != "librequake:sound/ambience/wind2.wav" {
		t.Errorf("wind2.wav's origin is %q: it has LibreQuake's own cue point and must be copied as is", o)
	}
}

// LoopingSounds are in the layout (a name that is not would be a silent no-op).
func TestLoopingSoundsAreInTheLayout(t *testing.T) {
	names, _, err := Layout()
	if err != nil {
		t.Fatal(err)
	}
	in := map[string]bool{}
	for _, n := range names {
		in[n] = true
	}
	for _, n := range LoopingSounds {
		if !in[n] {
			t.Errorf("%s is not a layout path", n)
		}
	}
}
