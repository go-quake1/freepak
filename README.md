# freepak

A freely licensed drop-in replacement for id Software's shareware `id1/pak0.pak`:
the same 339 paths, built from [LibreQuake](https://github.com/lavenderdotpet/LibreQuake)
plus a few files generated here. It exists so that `go-quake1/engine`, its
tests and demos have a data set that can be redistributed.

id's pak is used only for facts (its list of paths, in `layout.txt`, with sizes for
information) and, when you have it, to prove that none of its bytes are in the
output. Its content is never stored or copied.

## Get it

Release assets (`pak0.pak`, `MANIFEST`, `SHA256SUMS`, licence files) are on the
[releases page](https://github.com/go-quake1/freepak/releases). The data is not in git.

## Build it

```
go run ./cmd/freepak build --librequake lite.zip --reference id1/pak0.pak --out out/
go run ./cmd/freepak build --librequake lite.zip --no-reference --out out/
```

`lite.zip` is LibreQuake v0.09-beta (`gh release download v0.09-beta -R lavenderdotpet/LibreQuake -p lite.zip`);
the build refuses any archive whose sha256 is not
`428e736b2f01d953e09a08c60bee975bdc4a0ac2219e97fa095c8af41754da83`.
Two builds give identical bytes.

`--reference` or `--no-reference` is required. Without the reference the
MANIFEST says the reference gates did not run.

Other commands: `freepak maps --librequake lite.zip` lists LibreQuake's maps with
their changelevel targets; `freepak layout --reference pak0.pak --out layout.txt`
regenerates `layout.txt` (paths and sizes only).

## What is in the pak

| Entries | Origin |
|---|---|
| 329 paths | the LibreQuake entry with the same path (pak1 overrides pak0) |
| `maps/e1m1.bsp` ... `e1m8.bsp` | LibreQuake `lq_e0m1` ... `lq_e0m8`, same number |
| `end1.bin` | generated: an 80x25 text screen written for this project |
| `sound/player/slimbrn2.wav` | generated: synthesised by integer code (bubbles over low-passed noise), not sampled |

LibreQuake's lite set is a single episode: a hub (`maps/start.bsp`) leads to
`lq_e0m1`, which chains to `m2`, `m3`, `m4`, `m5`; `m5` leads to `m6` and to the
secret `m8` (which returns to `m6`); `m6` leads to `m7`, which returns to the hub.
`lq_e0m1`...`m7` are therefore the main line and `m8` the secret, the roles of
e1m1...e1m7 and e1m8, and LibreQuake's `progs.dat` itself carries the names
`e1m7` and `e1m8`. The maps are copied with one change, in their entity lump:
`trigger_changelevel` targets `lq_e0mN` become `e1mN` so that the episode chains
under its new names (`MANIFEST` marks these entries `+changelevels`). LibreQuake's
`lq_e0m2` also has an exit to a map that does not exist (`e0-level-trans`); it
is sent to `e1m3` like its other exit.

## Gates

`freepak build` fails closed unless: every layout path is present and nothing else;
every entry has an origin and a licence; the pak opens with the engine's readers
(every map with `bspfile`, `progs.dat` with `progs`, the eight episode maps with
`worldview.LoadMap`), every changelevel target is a map of the pak, and `e1m1`
renders with `worldview.DrawSoftware`. With `--reference`, additionally no entry has
the sha256 of a reference entry and no 4 KiB window of any output entry equals a
window of any reference entry (rolling hash at every offset on both sides; windows of a
single repeated byte are skipped).

One documented exception: `gfx/palette.lmp` is byte-identical to the reference's. It is the
256-colour palette that every Quake asset is indexed against (and LibreQuake ships it);
a different palette would recolour all assets. The gate lists the exception in the
MANIFEST and accepts no other.

## Licences

Mixed, as in LibreQuake: the models, textures, sounds and maps are BSD-3-Clause
(see `COPYING-LibreQuake` and `CREDITS-LibreQuake` in the release); `progs.dat`
is GPL-2.0-or-later (source: LibreQuake's `qcsrc/`, see `GPL-NOTICE`); the code in
this repository and the two generated files are BSD-3-Clause (`LICENSE`).
Quake is a trademark of id Software.
