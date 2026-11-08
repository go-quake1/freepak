package freepak

import (
	"bytes"
	"path"
	"sort"
	"strings"

	"github.com/go-quake1/engine/bspfile"
	"github.com/go-quake1/engine/entparse"
)

// MapInfo summarises what a map's entity lump says about how it plays.
type MapInfo struct {
	Name     string   // "lq_e1m1", without maps/ and .bsp
	Title    string   // worldspawn "message"
	Start    bool     // has an info_player_start
	Monsters int      // monster_* entities
	Items    int      // item_* and weapon_* entities
	Exits    []string // trigger_changelevel "map" targets, sorted, deduplicated
	Size     int      // file size
}

// InspectMaps reads every maps/*.bsp in files.
func InspectMaps(files []File) ([]MapInfo, error) {
	var out []MapInfo
	for _, f := range files {
		if path.Dir(f.Name) != "maps" || !strings.HasSuffix(f.Name, ".bsp") {
			continue
		}
		bf, err := bspfile.Open(bytes.NewReader(f.Data), int64(len(f.Data)))
		if err != nil {
			return nil, err
		}
		ents, err := entparse.ParseEntities(bf.Entities())
		if err != nil {
			return nil, err
		}
		mi := MapInfo{Name: strings.TrimSuffix(path.Base(f.Name), ".bsp"), Size: len(f.Data)}
		exits := map[string]bool{}
		for _, e := range ents {
			cn := e["classname"]
			switch {
			case cn == "worldspawn":
				mi.Title = e["message"]
			case cn == "info_player_start":
				mi.Start = true
			case strings.HasPrefix(cn, "monster_"):
				mi.Monsters++
			case strings.HasPrefix(cn, "item_"), strings.HasPrefix(cn, "weapon_"):
				mi.Items++
			case cn == "trigger_changelevel":
				exits[e["map"]] = true
			}
		}
		for m := range exits {
			mi.Exits = append(mi.Exits, m)
		}
		sort.Strings(mi.Exits)
		out = append(out, mi)
	}
	return out, nil
}
