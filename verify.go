package freepak

import (
	"bytes"
	"fmt"
	"path"
	"strings"

	"github.com/go-quake1/engine/assets"
	"github.com/go-quake1/engine/bspfile"
	"github.com/go-quake1/engine/pak"
	"github.com/go-quake1/engine/progs"
	"github.com/go-quake1/engine/render"
	"github.com/go-quake1/engine/vfs"
	"github.com/go-quake1/engine/worldview"
)

// EngineReport is what opening the pak with go-quake1/engine measured.
type EngineReport struct {
	Entries     int
	MapsOpened  int
	MapsLoaded  int // episode maps worldview.LoadMap accepted
	ProgsLoads  bool
	Views       int     // views of e1m1 drawn with worldview.DrawSoftware
	MinCoverage float64 // smallest fraction of pixels a view changed from the clear colour
	Exits       int     // trigger_changelevel targets checked
}

// VerifyEngine opens pakBytes with the engine's readers: the pak, every map
// with bspfile, progs.dat with progs, the episode maps with worldview.LoadMap,
// and draws every view of maps/e1m1.bsp with worldview.DrawSoftware. It also
// checks that every changelevel target of every map is a map of the pak.
func VerifyEngine(pakBytes []byte) (EngineReport, error) {
	var r EngineReport
	fsys, err := pak.Open(bytes.NewReader(pakBytes))
	if err != nil {
		return r, err
	}
	files, err := ReadPak(pakBytes)
	if err != nil {
		return r, err
	}
	r.Entries = len(files)
	have := map[string]bool{}
	for _, f := range files {
		have[f.Name] = true
	}
	for _, f := range files {
		switch {
		case f.Name == "progs.dat":
			if _, err := progs.Load(bytes.NewReader(f.Data), int64(len(f.Data))); err != nil {
				return r, fmt.Errorf("progs.dat: %w", err)
			}
			r.ProgsLoads = true
		case path.Dir(f.Name) == "maps" && strings.HasSuffix(f.Name, ".bsp"):
			if _, err := bspfile.Open(bytes.NewReader(f.Data), int64(len(f.Data))); err != nil {
				return r, fmt.Errorf("%s: %w", f.Name, err)
			}
			r.MapsOpened++
		}
	}
	if !r.ProgsLoads {
		return r, fmt.Errorf("no progs.dat")
	}
	infos, err := InspectMaps(files)
	if err != nil {
		return r, err
	}
	for _, m := range infos {
		for _, e := range m.Exits {
			r.Exits++
			if !have["maps/"+e+".bsp"] {
				return r, fmt.Errorf("map %s: changelevel target %q is not a map of the pak", m.Name, e)
			}
		}
	}
	for _, slug := range EpisodeMaps {
		if _, err := worldview.LoadMap(fsys, slug); err != nil {
			return r, fmt.Errorf("worldview.LoadMap(%s): %w", slug, err)
		}
		r.MapsLoaded++
	}
	w, err := worldview.LoadMap(fsys, "e1m1")
	if err != nil {
		return r, err
	}
	v := vfs.New()
	v.Add(fsys)
	cm, err := assets.LoadColorMapFrom(v)
	if err != nil {
		return r, err
	}
	r.MinCoverage = 1
	for _, view := range w.Views() {
		fb, err := render.NewFrameBuffer(320, 200)
		if err != nil {
			return r, err
		}
		if err := w.DrawSoftware(fb, cm, view); err != nil {
			return r, fmt.Errorf("DrawSoftware: %w", err)
		}
		drawn := 0
		for y := 0; y < 200; y++ {
			for x := 0; x < 320; x++ {
				if fb.Pixels[y*fb.Pitch+x] != worldview.ClearIndex {
					drawn++
				}
			}
		}
		cov := float64(drawn) / (320 * 200)
		r.MinCoverage = min(r.MinCoverage, cov)
		r.Views++
	}
	if r.Views == 0 {
		return r, fmt.Errorf("e1m1 has no views to draw")
	}
	return r, nil
}
