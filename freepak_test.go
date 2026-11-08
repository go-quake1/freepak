package freepak

import (
	"bytes"
	"encoding/binary"
	"math/rand/v2"
	"os"
	"strings"
	"testing"
)

// Inputs that are not in the repository: the pinned LibreQuake archive
// (FREEPAK_LQ) and, locally only, id's pak (QUAKE_PAK). FREEPAK_REQUIRE=1 (set
// in CI) turns a missing input into a failure instead of a skip.
func input(t *testing.T, env string) string {
	t.Helper()
	p := os.Getenv(env)
	if p == "" {
		if os.Getenv("FREEPAK_REQUIRE") == "1" && env == "FREEPAK_LQ" {
			t.Fatalf("%s not set but FREEPAK_REQUIRE=1", env)
		}
		t.Skipf("%s not set", env)
	}
	return p
}

func noise(seed uint64, n int) []byte {
	r := rand.New(rand.NewPCG(seed, 1))
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(r.Uint32())
	}
	return b
}

func TestPakRoundTrip(t *testing.T) {
	in := []File{{"a.txt", []byte("hello")}, {"maps/e1m1.bsp", noise(1, 1000)}, {"empty", nil}}
	b, err := WritePak(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := ReadPak(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(in) {
		t.Fatalf("%d entries", len(out))
	}
	for i := range in {
		if out[i].Name != in[i].Name || !bytes.Equal(out[i].Data, in[i].Data) {
			t.Errorf("entry %d differs", i)
		}
	}
	b2, _ := WritePak(in)
	if !bytes.Equal(b, b2) {
		t.Error("WritePak is not deterministic")
	}
}

func TestReadPakRefusesBadInput(t *testing.T) {
	good, _ := WritePak([]File{{"a", []byte("xyz")}})
	mut := func(f func(b []byte)) []byte {
		b := bytes.Clone(good)
		f(b)
		return b
	}
	dir := len(good) - entrySize
	cases := map[string][]byte{
		"short":      good[:5],
		"magic":      mut(func(b []byte) { b[0] = 'X' }),
		"dirofs":     mut(func(b []byte) { binary.LittleEndian.PutUint32(b[4:], 1<<30) }),
		"dirlen":     mut(func(b []byte) { binary.LittleEndian.PutUint32(b[8:], 65) }),
		"negofs":     mut(func(b []byte) { binary.LittleEndian.PutUint32(b[dir+nameSize:], 0xFFFFFFF0) }),
		"neglen":     mut(func(b []byte) { binary.LittleEndian.PutUint32(b[dir+nameSize+4:], 0xFFFFFFFF) }),
		"pastend":    mut(func(b []byte) { binary.LittleEndian.PutUint32(b[dir+nameSize+4:], 1000) }),
		"emptyname":  mut(func(b []byte) { b[dir] = 0 }),
		"dotdot":     mut(func(b []byte) { copy(b[dir:], "../x\x00") }),
		"slashfirst": mut(func(b []byte) { copy(b[dir:], "/x\x00") }),
	}
	for name, b := range cases {
		if _, err := ReadPak(b); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := WritePak([]File{{"../x", nil}}); err == nil {
		t.Error("WritePak accepted ../x")
	}
	if _, err := ReadPak(mut(func(b []byte) {})); err != nil {
		t.Errorf("control: %v", err)
	}
}

func fakeBSP(ents string, other []byte) []byte {
	out := make([]byte, bspHeader)
	binary.LittleEndian.PutUint32(out, bspVersion)
	for i, data := range [][]byte{[]byte(ents), other} {
		for len(out)%4 != 0 {
			out = append(out, 0)
		}
		binary.LittleEndian.PutUint32(out[4+i*8:], uint32(len(out)))
		binary.LittleEndian.PutUint32(out[8+i*8:], uint32(len(data)))
		out = append(out, data...)
	}
	for i := 2; i < numLumps; i++ { // remaining lumps empty at the end
		binary.LittleEndian.PutUint32(out[4+i*8:], uint32(len(out)))
	}
	return out
}

func TestRenameChangelevels(t *testing.T) {
	other := noise(7, 333)
	ents := "{\n\"classname\" \"trigger_changelevel\"\n\"map\" \"lq_e0m2\"\n}\n{\n\"classname\" \"trigger_changelevel\"\n\"map\" \"lq_e0m22\"\n}\n"
	in := fakeBSP(ents, other)
	out, n, err := RenameChangelevels(in, map[string]string{"lq_e0m2": "e1m2"})
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	ofs := binary.LittleEndian.Uint32(out[4:])
	ln := binary.LittleEndian.Uint32(out[8:])
	got := string(out[ofs : ofs+ln])
	if !strings.Contains(got, `"map" "e1m2"`) || !strings.Contains(got, `"map" "lq_e0m22"`) {
		t.Errorf("entities: %q", got)
	}
	ofs1 := binary.LittleEndian.Uint32(out[12:])
	ln1 := binary.LittleEndian.Uint32(out[16:])
	if !bytes.Equal(out[ofs1:ofs1+ln1], other) {
		t.Error("other lump changed")
	}
	same, n, err := RenameChangelevels(in, map[string]string{"zzz": "y"})
	if err != nil || n != 0 || !bytes.Equal(same, in) {
		t.Error("no-op rewrite changed the file")
	}
	if _, _, err := RenameChangelevels(in[:50], nil); err == nil {
		t.Error("short file accepted")
	}
	bad := bytes.Clone(in)
	binary.LittleEndian.PutUint32(bad[8:], 1<<20)
	if _, _, err := RenameChangelevels(bad, nil); err == nil {
		t.Error("out-of-range lump accepted")
	}
	bad = bytes.Clone(in)
	bad[0] = 30
	if _, _, err := RenameChangelevels(bad, nil); err == nil {
		t.Error("wrong version accepted")
	}
}

func TestGenerated(t *testing.T) {
	e := End1Bin()
	if len(e) != 4000 || !bytes.Equal(e, End1Bin()) {
		t.Fatalf("end1.bin: %d bytes or not deterministic", len(e))
	}
	if !bytes.Contains(e, []byte("F\x4f \x4fR")) {
		t.Error("end1.bin: title not found")
	}
	w := Slimbrn2Wav()
	if !bytes.Equal(w, Slimbrn2Wav()) {
		t.Fatal("wav not deterministic")
	}
	if string(w[:4]) != "RIFF" || string(w[8:16]) != "WAVEfmt " || string(w[36:40]) != "data" {
		t.Fatal("wav header")
	}
	if binary.LittleEndian.Uint32(w[4:]) != uint32(len(w)-8) || binary.LittleEndian.Uint32(w[40:]) != uint32(len(w)-44) {
		t.Error("wav sizes")
	}
	lo, hi := byte(255), byte(0)
	for _, s := range w[44:] {
		lo, hi = min(lo, s), max(hi, s)
	}
	if hi-lo < 40 {
		t.Errorf("wav is nearly silent: %d..%d", lo, hi)
	}
	if lo == 0 || hi == 255 {
		t.Errorf("wav clips: %d..%d", lo, hi)
	}
}

func TestLayout(t *testing.T) {
	names, sizes, err := Layout()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 339 || len(sizes) != 339 {
		t.Fatalf("%d paths", len(names))
	}
	seen := map[string]bool{}
	for _, n := range names {
		if seen[n] {
			t.Errorf("duplicate %s", n)
		}
		seen[n] = true
		if err := checkName(n); err != nil {
			t.Error(err)
		}
	}
	for _, want := range []string{"progs.dat", "gfx/palette.lmp", "end1.bin", "sound/player/slimbrn2.wav", "maps/e1m1.bsp", "maps/e1m8.bsp", "maps/start.bsp"} {
		if !seen[want] {
			t.Errorf("layout lacks %s", want)
		}
	}
}

// The gate must see what it is for: a planted run of reference bytes.
func TestGatePlantedPositive(t *testing.T) {
	ref := []File{{"r1", noise(10, 20000)}, {"r2", noise(11, 9000)}, {"tiny", []byte("abc")}}
	clean := []File{{"o1", noise(20, 30000)}}
	rep, err := CheckNoReferenceBytes(clean, ref, "x")
	if err != nil || rep.WindowHits != 0 || rep.OutWindows == 0 || rep.RefWindows == 0 {
		t.Fatalf("clean output: %+v %v", rep, err)
	}
	// An exactly-4096-byte run from the middle of a reference entry, at an odd offset.
	run := ref[0].Data[5001 : 5001+Window]
	planted := noise(21, 3000)
	planted = append(planted, run...)
	planted = append(planted, noise(22, 3000)...)
	rep, err = CheckNoReferenceBytes([]File{{"o2", planted}}, ref, "x")
	if err == nil || rep.WindowHits != 1 {
		t.Fatalf("planted 4096-byte run: hits=%d err=%v", rep.WindowHits, err)
	}
	// One byte short of a window is not detected (the gate's stated size).
	rep, err = CheckNoReferenceBytes([]File{{"o3", append(noise(23, 100), ref[0].Data[:Window-1]...)}}, ref, "x")
	if err != nil || rep.WindowHits != 0 {
		t.Fatalf("4095-byte run: %+v %v", rep, err)
	}
	// An identical entry, small or large, is refused under any name.
	for _, f := range []File{{"copy", ref[2].Data}, {"copy", ref[1].Data}} {
		if _, err := CheckNoReferenceBytes([]File{f}, ref, "x"); err == nil {
			t.Errorf("identical %d-byte entry accepted", len(f.Data))
		}
	}
	// Padding windows (one repeated byte) are not evidence.
	if _, err := CheckNoReferenceBytes([]File{{"z", make([]byte, 12000)}}, []File{{"z", make([]byte, 10000)}}, "x"); err != nil {
		t.Errorf("zero padding flagged: %v", err)
	}
}

func TestGateException(t *testing.T) {
	pal := noise(30, 768)
	ref := []File{{"gfx/palette.lmp", pal}, {"other.lmp", pal[:100]}}
	// There is no exception any more: an identical palette is refused like any entry.
	if _, err := CheckNoReferenceBytes([]File{{"gfx/palette.lmp", pal}}, ref, "x"); err == nil {
		t.Error("identical palette accepted")
	}
	if _, err := CheckNoReferenceBytes([]File{{"other.lmp", pal[:100]}}, ref, "x"); err == nil {
		t.Error("identical entry accepted")
	}
	own := OwnPalette()
	if n, err := CheckPalette([]File{{"gfx/palette.lmp", own}}, ref); err != nil || n > MaxSamePaletteColours {
		t.Errorf("own palette against noise: %d %v", n, err)
	}
	if _, err := CheckPalette([]File{{"gfx/palette.lmp", pal}}, ref); err == nil {
		t.Error("CheckPalette accepted the identical palette")
	}
	if _, err := CheckPalette(nil, ref); err == nil {
		t.Error("CheckPalette accepted a missing palette")
	}
}

func TestOpenLibreQuakeRefusesOtherArchive(t *testing.T) {
	p := t.TempDir() + "/x.zip"
	if err := os.WriteFile(p, []byte("not the pinned archive"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenLibreQuake(p); err == nil || !strings.Contains(err.Error(), "pinned") {
		t.Fatalf("err=%v", err)
	}
	if _, err := OpenLibreQuake(p + ".missing"); err == nil {
		t.Error("missing file accepted")
	}
}

func TestBuildFlagsExclusive(t *testing.T) {
	for _, o := range []Options{{}, {Reference: "x", NoReference: true}} {
		if _, err := Build(o, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "exactly one") {
			t.Errorf("%+v: %v", o, err)
		}
	}
}

// build runs the whole build from the pinned archive without the reference.
func buildLQ(t *testing.T, ref string) *Result {
	t.Helper()
	opt := Options{LibreQuake: input(t, "FREEPAK_LQ"), OutDir: t.TempDir()}
	if ref == "" {
		opt.NoReference = true
	} else {
		opt.Reference = ref
	}
	res, err := Build(opt, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestBuildFromLibreQuake(t *testing.T) {
	a := buildLQ(t, "")
	b := buildLQ(t, "")
	if !bytes.Equal(a.Pak, b.Pak) || !bytes.Equal(a.Manifest, b.Manifest) {
		t.Fatal("two builds differ: not reproducible")
	}
	names, _, _ := Layout()
	files, err := ReadPak(a.Pak)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != len(names) || len(a.Entries) != len(names) {
		t.Fatalf("%d entries, layout %d", len(files), len(names))
	}
	gen, lq, gpl := 0, 0, 0
	for i, f := range files {
		e := a.Entries[i]
		if f.Name != names[i] || e.Path != names[i] {
			t.Fatalf("entry %d is %s, layout says %s", i, f.Name, names[i])
		}
		switch {
		case strings.HasPrefix(e.Origin, "generated:"):
			gen++
		case strings.HasPrefix(e.Origin, "librequake:"):
			lq++
		default:
			t.Errorf("%s: origin %q", e.Path, e.Origin)
		}
		if e.Licence != LicBSD3 && e.Licence != LicGPL2 {
			t.Errorf("%s: licence %q", e.Path, e.Licence)
		}
		if e.Licence == LicGPL2 {
			gpl++
		}
	}
	if gen != 4 || gpl != 1 || lq != 335 {
		t.Errorf("origins: %d generated, %d librequake, %d GPL", gen, lq, gpl)
	}
	if a.Engine.MapsLoaded != 8 || !a.Engine.ProgsLoads || a.Engine.Views == 0 {
		t.Errorf("engine: %+v", a.Engine)
	}
	if !bytes.Contains(a.Manifest, []byte("NOT RUN")) {
		t.Error("manifest does not stamp the missing reference gates")
	}
	if bytes.Contains(a.Manifest, []byte("EXCEPTION")) {
		t.Error("manifest carries an exception")
	}
	// Quantisation report: the reindexing error over all texels.
	if a.Quant.Texels < 1e6 || a.Quant.Mean > 4 || a.Quant.Max > 30 {
		t.Errorf("quantisation: %+v", a.Quant)
	}
	t.Logf("quantisation: %+v", a.Quant)
	// The palette is ours and the colormap has all 16385 bytes.
	for _, f := range files {
		switch f.Name {
		case "gfx/palette.lmp":
			if !bytes.Equal(f.Data, OwnPalette()) {
				t.Error("palette.lmp is not OwnPalette")
			}
		case "gfx/colormap.lmp":
			if len(f.Data) != 16385 || !bytes.Equal(f.Data, Colormap(OwnPalette())) {
				t.Errorf("colormap.lmp: %d bytes", len(f.Data))
			}
		}
	}
	// Cross-architecture pin: the pak that was released from this source is the
	// one this machine builds (floating point must not leak into the bytes).
	want, err := os.ReadFile("PAK0.SHA256")
	if err != nil {
		t.Fatal(err)
	}
	if got := a.PakSHA256; strings.TrimSpace(string(want)) != got {
		t.Errorf("pak0.pak sha256 %s, PAK0.SHA256 says %s", got, strings.TrimSpace(string(want)))
	}
}

// With id's pak (local only): the no-copy gates.
func TestBuildAgainstReference(t *testing.T) {
	res := buildLQ(t, input(t, "QUAKE_PAK"))
	if res.Ref == nil || res.Ref.SameSHA != 0 || res.Ref.WindowHits != 0 || res.Ref.OutWindows == 0 {
		t.Fatalf("%+v", res.Ref)
	}
	t.Logf("reference gates: %+v", *res.Ref)
}
