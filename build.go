package freepak

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

//go:embed layout.txt
var layoutTxt string

//go:embed LICENSE
var freepakLicence string

// EpisodeMaps are the output paths filled with LibreQuake maps, in order.
var EpisodeMaps = []string{"e1m1", "e1m2", "e1m3", "e1m4", "e1m5", "e1m6", "e1m7", "e1m8"}

// episodeSource maps each episode slot to the LibreQuake map it carries.
//
// LibreQuake's lite set is one episode, "e0": a hub (maps/start.bsp) that
// leads to lq_e0m1; lq_e0m1 -> m2 -> m3 -> m4 -> m5; m5 leads to m6 and to the
// secret map m8 (which returns to m6); m6 -> m7; m7 returns to start. So
// lq_e0m1..m7 are the main line in order and lq_e0m8 is the secret, exactly
// the roles of e1m1..e1m7 and e1m8 in id's first episode, and LibreQuake's
// progs.dat even carries the names e1m7 and e1m8 for those two. Each slot gets
// the LibreQuake map of the same number.
var episodeSource = map[string]string{
	"e1m1": "lq_e0m1", "e1m2": "lq_e0m2", "e1m3": "lq_e0m3", "e1m4": "lq_e0m4",
	"e1m5": "lq_e0m5", "e1m6": "lq_e0m6", "e1m7": "lq_e0m7", "e1m8": "lq_e0m8",
}

// The changelevel targets are rewritten to the new names so that the episode
// chains in the pak: lq_e0mN becomes e1mN, and lq_e0m2's second exit, the
// dangling "e0-level-trans" (no such map exists in LibreQuake), goes to e1m3
// like its other exit.
func changelevelRenames() map[string]string {
	m := map[string]string{"e0-level-trans": "e1m3"}
	for slot, src := range episodeSource {
		m[src] = slot
	}
	return m
}

// Licence identifiers of the manifest.
const (
	LicBSD3 = "BSD-3-Clause"
	LicGPL2 = "GPL-2.0-or-later"
)

// Entry is a MANIFEST line.
type Entry struct {
	Path    string
	SHA256  string
	Size    int
	Origin  string // "librequake:<path in LibreQuake>[+changelevels]" or "generated:<what>"
	Licence string
}

// Layout returns the paths (and the sizes of the reference, for information) of
// the reference pak, in its order.
func Layout() ([]string, []int, error) {
	var names []string
	var sizes []int
	sc := bufio.NewScanner(strings.NewReader(layoutTxt))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 2 {
			return nil, nil, fmt.Errorf("layout.txt: bad line %q", line)
		}
		n, err := strconv.Atoi(f[1])
		if err != nil {
			return nil, nil, fmt.Errorf("layout.txt: %w", err)
		}
		names = append(names, f[0])
		sizes = append(sizes, n)
	}
	return names, sizes, sc.Err()
}

// FormatLayout renders the layout file for a reference pak: its paths and
// sizes, nothing else of its content.
func FormatLayout(ref []File) []byte {
	var b bytes.Buffer
	b.WriteString("# The paths of the reference id1/pak0.pak, in its order, with their sizes (for information).\n")
	b.WriteString("# Facts only: no content of the reference is stored here.\n")
	for _, f := range ref {
		fmt.Fprintf(&b, "%s %d\n", f.Name, len(f.Data))
	}
	return b.Bytes()
}

// Options configures Build.
type Options struct {
	LibreQuake  string // path of lite.zip
	Reference   string // path of the reference pak; "" with NoReference
	NoReference bool   // run without the reference gates; stamped in the MANIFEST
	OutDir      string
}

// Result is a finished build.
type Result struct {
	Pak, Manifest []byte
	Entries       []Entry
	PakSHA256     string
	Ref           *RefReport // nil when the reference gates did not run
	Engine        EngineReport
	Quant         Quantisation
}

// Assemble builds the pak and its manifest in memory from the LibreQuake
// content, in the reference layout. Every layout path must be produced, and
// nothing else.
func Assemble(lq *LibreQuake) (_ []File, _ []Entry, q Quantisation, err error) {
	names, _, err := Layout()
	if err != nil {
		return nil, nil, q, err
	}
	if len(names) == 0 {
		return nil, nil, q, errors.New("empty layout")
	}
	oldPal, ok := lq.Lookup("gfx/palette.lmp")
	if !ok || len(oldPal) != 768 {
		return nil, nil, q, errors.New("librequake has no 768-byte gfx/palette.lmp: the source colours of its assets")
	}
	newPal := OwnPalette()
	remap := NewRemap(oldPal, newPal)
	rename := changelevelRenames()
	var files []File
	var entries []Entry
	var addErr error
	// add records an entry. Content that comes from LibreQuake is copied and
	// reindexed from LibreQuake's palette to ours.
	add := func(name string, data []byte, origin, lic string) {
		if strings.HasPrefix(origin, "librequake:") {
			data = bytes.Clone(data)
			did, err := ReindexEntry(name, data, remap, newPal)
			if err != nil && addErr == nil {
				addErr = err
			}
			if did {
				origin += "+reindexed"
			}
		}
		sum := sha256.Sum256(data)
		files = append(files, File{Name: name, Data: data})
		entries = append(entries, Entry{name, hex.EncodeToString(sum[:]), len(data), origin, lic})
	}
	defer func() {
		if err == nil {
			err = addErr
		}
		q = remap.Stats()
	}()
	var missing []string
	for _, name := range names {
		switch name {
		case "gfx/palette.lmp":
			add(name, newPal, "generated:palette.lmp", LicBSD3)
			continue
		case "gfx/colormap.lmp":
			add(name, Colormap(newPal), "generated:colormap.lmp", LicBSD3)
			continue
		}
		if src, ok := episodeFor(name); ok {
			data, found := lq.Lookup("maps/" + src + ".bsp")
			if !found {
				return nil, nil, q, fmt.Errorf("librequake has no maps/%s.bsp", src)
			}
			out, _, err := RenameChangelevels(data, rename)
			if err != nil {
				return nil, nil, q, err
			}
			add(name, out, "librequake:maps/"+src+".bsp+changelevels", LicBSD3)
			continue
		}
		switch name {
		case "end1.bin":
			add(name, End1Bin(), "generated:end1.bin", LicBSD3)
			continue
		case "sound/player/slimbrn2.wav":
			add(name, Slimbrn2Wav(), "generated:slimbrn2.wav", LicBSD3)
			continue
		}
		data, found := lq.Lookup(name)
		if !found {
			missing = append(missing, name)
			continue
		}
		lic := LicBSD3
		if name == "progs.dat" {
			lic = LicGPL2
		}
		origin := "librequake:" + name
		if strings.HasPrefix(name, "maps/") && strings.HasSuffix(name, ".bsp") {
			out, n, err := RenameChangelevels(data, rename)
			if err != nil {
				return nil, nil, q, err
			}
			if n > 0 {
				data, origin = out, origin+"+changelevels"
			}
		}
		add(name, data, origin, lic)
	}
	if len(missing) > 0 {
		return nil, nil, q, fmt.Errorf("layout paths with no source: %v", missing)
	}
	return files, entries, q, nil
}

func episodeFor(name string) (string, bool) {
	if !strings.HasPrefix(name, "maps/") || !strings.HasSuffix(name, ".bsp") {
		return "", false
	}
	src, ok := episodeSource[strings.TrimSuffix(strings.TrimPrefix(name, "maps/"), ".bsp")]
	return src, ok
}

// FormatManifest renders the MANIFEST: one tab-separated line per entry,
// preceded by comment lines that state the gates behind it.
func FormatManifest(entries []Entry, ref *RefReport, eng EngineReport, q Quantisation, pakSHA string) []byte {
	var b bytes.Buffer
	b.WriteString("# freepak MANIFEST: path, sha256, size, origin, licence (tab separated)\n")
	fmt.Fprintf(&b, "# palette: generated by freepak from its own OKLab ramp parameters; every indexed asset reindexed to it; %d texels, OKLab delta-E (x100) mean %.2f, max %.2f (old index %d)\n", q.Texels, q.Mean, q.Max, q.MaxIdx)
	fmt.Fprintf(&b, "# pak0.pak sha256 %s, %d entries\n", pakSHA, len(entries))
	fmt.Fprintf(&b, "# librequake archive lite.zip sha256 %s\n", LibreQuakeSHA256)
	fmt.Fprintf(&b, "# engine check: %d maps open, %d episode maps load, progs.dat loads, e1m1 drawn from %d views (min coverage %.3f), %d changelevel targets resolve\n",
		eng.MapsOpened, eng.MapsLoaded, eng.Views, eng.MinCoverage, eng.Exits)
	if ref == nil {
		b.WriteString("# reference gates: NOT RUN (built with --no-reference); this build is not certified free of reference bytes\n")
	} else {
		fmt.Fprintf(&b, "# reference gates: passed against a reference of %d entries (pak sha256 %s): %d entries with a reference sha256, %d of %d 4 KiB output windows matching %d reference windows\n",
			ref.RefEntries, ref.RefSHA256, ref.SameSHA, ref.WindowHits, ref.OutWindows, ref.RefWindows)
		fmt.Fprintf(&b, "# reference palette: %d of 256 colours equal by value (at most %d allowed); no exception: no entry equals a reference entry\n", ref.SameColours, MaxSamePaletteColours)
	}
	for _, e := range entries {
		fmt.Fprintf(&b, "%s\t%s\t%d\t%s\t%s\n", e.Path, e.SHA256, e.Size, e.Origin, e.Licence)
	}
	return b.Bytes()
}

// gplNotice is the notice shipped next to the pak.
const gplNotice = `GPL notice
==========

progs.dat in pak0.pak is licensed GPL-2.0-or-later (see COPYING-GPL-2). It is
LibreQuake's compiled game code (LibreProgsDeluxe, based on the GPL quakec 1.01
that id Software released).

Source: https://github.com/lavenderdotpet/LibreQuake, directory qcsrc/
(release v0.09-beta, lite.zip sha256 ` + LibreQuakeSHA256 + `).
The qcsrc tree is the corresponding source of progs.dat.

Every other entry is BSD-3-Clause: the models, textures, sounds and maps are
LibreQuake's (COPYING-LibreQuake, CREDITS-LibreQuake), and end1.bin and
sound/player/slimbrn2.wav are generated by freepak (COPYING). The maps' entity
lumps differ from LibreQuake's only in the changelevel targets, see MANIFEST.
`

// Build assembles, verifies and writes the release files into opt.OutDir.
func Build(opt Options, log io.Writer) (*Result, error) {
	if (opt.Reference == "") == !opt.NoReference {
		return nil, errors.New("give exactly one of --reference <id pak0> or --no-reference")
	}
	lq, err := OpenLibreQuake(opt.LibreQuake)
	if err != nil {
		return nil, err
	}
	files, entries, quant, err := Assemble(lq)
	if err != nil {
		return nil, err
	}
	pakBytes, err := WritePak(files)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(pakBytes)
	res := &Result{Pak: pakBytes, Entries: entries, PakSHA256: hex.EncodeToString(sum[:]), Quant: quant}
	fmt.Fprintf(log, "quantisation: %+v\n", quant)
	for _, e := range entries {
		if e.Origin == "" || e.Licence == "" {
			return nil, fmt.Errorf("%s has no declared origin or licence", e.Path)
		}
	}
	if res.Engine, err = VerifyEngine(pakBytes); err != nil {
		return nil, fmt.Errorf("engine check: %w", err)
	}
	fmt.Fprintf(log, "engine: %+v\n", res.Engine)
	if opt.Reference != "" {
		raw, err := os.ReadFile(opt.Reference)
		if err != nil {
			return nil, err
		}
		ref, err := ReadPak(raw)
		if err != nil {
			return nil, fmt.Errorf("reference: %w", err)
		}
		if err := checkLayout(ref); err != nil {
			return nil, err
		}
		rs := sha256.Sum256(raw)
		rep, err := CheckNoReferenceBytes(files, ref, hex.EncodeToString(rs[:]))
		if err != nil {
			return nil, err
		}
		if rep.SameColours, err = CheckPalette(files, ref); err != nil {
			return nil, err
		}
		res.Ref = &rep
		fmt.Fprintf(log, "reference gates: %+v\n", rep)
	} else {
		fmt.Fprintln(log, "reference gates: NOT RUN (--no-reference)")
	}
	res.Manifest = FormatManifest(entries, res.Ref, res.Engine, res.Quant, res.PakSHA256)
	if err := writeOut(opt.OutDir, res, lq); err != nil {
		return nil, err
	}
	return res, nil
}

// checkLayout verifies that the embedded layout is the reference's.
func checkLayout(ref []File) error {
	names, sizes, err := Layout()
	if err != nil {
		return err
	}
	if len(names) != len(ref) {
		return fmt.Errorf("layout.txt has %d paths, the reference %d", len(names), len(ref))
	}
	for i, f := range ref {
		if names[i] != f.Name || sizes[i] != len(f.Data) {
			return fmt.Errorf("layout.txt line %d (%s %d) differs from the reference (%s %d)", i, names[i], sizes[i], f.Name, len(f.Data))
		}
	}
	return nil
}

func writeOut(dir string, res *Result, lq *LibreQuake) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	sums := fmt.Sprintf("%s  pak0.pak\n%x  MANIFEST\n", res.PakSHA256, sha256.Sum256(res.Manifest))
	out := map[string][]byte{
		"pak0.pak":                      res.Pak,
		"MANIFEST":                      res.Manifest,
		"SHA256SUMS":                    []byte(sums),
		"COPYING":                       []byte(freepakLicence),
		"GPL-NOTICE":                    []byte(gplNotice),
		"COPYING-LibreQuake":            lq.Docs["docs/COPYING"],
		"CREDITS-LibreQuake":            lq.Docs["docs/CREDITS"],
		"README-IMPORTANT-LICENCE-INFO": lq.Docs["docs/README-IMPORTANT-LICENCE-INFO"],
		"COPYING-GPL-2":                 lq.Docs["docs/misc-docs/COPYING"],
		"LibreProgsDeluxe.md":           lq.Docs["docs/misc-docs/LibreProgsDeluxe.md"],
	}
	var names []string
	for n := range out {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), out[n], 0o644); err != nil {
			return err
		}
	}
	return nil
}
