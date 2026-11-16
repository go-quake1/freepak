package freepak

import (
	"bytes"
	"encoding/binary"
	"errors"
)

// LoopingSounds are the samples the game must be able to loop, by pak path.
// tyrquake loops a channel only when its sample has a loop point (a cue
// chunk): snd_mix.c S_PaintChannels restarts a channel at sc->loopstart at the
// end of its data when `ch->looping && sc->loopstart >= 0`, and otherwise ends
// it; snd_dma.c S_StaticSound refuses a static sound whose loopstart is -1
// ("Sound %s not looped"). So a sample that must loop without a cue chunk plays
// once and the level goes quiet. The list is:
//
//   - the two ambient channels of S_Init (snd_dma.c): ambience/water1.wav and
//     ambience/wind2.wav, whose volume S_UpdateAmbientSounds sets from the leaf
//     the listener is in, forever;
//   - what the ambientsound() static sounds of this pak's maps play: the
//     entities ambient_comp_hum (ambience/comp1.wav), ambient_drip (drip1.wav),
//     ambient_drone (drone6.wav) and the wall torches and flames (fire1.wav).
//
// ambience/thunder1.wav and ambience/windfly.wav have no cue point either, and
// no map of this pak starts them as static sounds: they are not on the list.
var LoopingSounds = []string{
	"sound/ambience/water1.wav",
	"sound/ambience/wind2.wav",
	"sound/ambience/comp1.wav",
	"sound/ambience/drip1.wav",
	"sound/ambience/drone6.wav",
	"sound/ambience/fire1.wav",
}

// ErrNotWAV is returned for data that is not a RIFF/WAVE file.
var ErrNotWAV = errors.New("freepak: not a RIFF/WAVE file")

// HasCue reports whether a WAV has a "cue " chunk, the loop point the engine
// reads (GetWavinfo: the first cue point's sample offset is the loop start).
func HasCue(wav []byte) (bool, error) {
	if len(wav) < 12 || string(wav[:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
		return false, ErrNotWAV
	}
	for pos := 12; pos+8 <= len(wav); {
		size := int(binary.LittleEndian.Uint32(wav[pos+4:]))
		if string(wav[pos:pos+4]) == "cue " {
			return true, nil
		}
		pos += 8 + size + size%2
	}
	return false, nil
}

// EnsureLoop returns wav with a cue chunk whose one cue point is sample 0
// (the whole sample loops: a seamless wrap is the recording's, as LibreQuake's
// own ambient samples that carry a cue point are), and reports whether it added
// one. A WAV that already has a cue chunk is returned as is. The chunk is the
// 28 bytes GetWavinfo reads: a count, then a 24-byte cue point (id, position,
// "data", chunk start, block start, sample offset), appended after the existing
// chunks, with the RIFF size updated.
func EnsureLoop(wav []byte) ([]byte, bool, error) {
	has, err := HasCue(wav)
	if err != nil || has {
		return wav, false, err
	}
	var cue bytes.Buffer
	cue.WriteString("cue ")
	put := func(v uint32) { _ = binary.Write(&cue, binary.LittleEndian, v) }
	put(28) // chunk size: count + one cue point
	put(1)  // cue points
	put(1)  // id
	put(0)  // position
	cue.WriteString("data")
	put(0) // chunk start
	put(0) // block start
	put(0) // sample offset: the loop starts at the first sample
	out := bytes.Clone(wav)
	if len(out)%2 == 1 {
		out = append(out, 0) // chunks are even-aligned
	}
	out = append(out, cue.Bytes()...)
	binary.LittleEndian.PutUint32(out[4:], uint32(len(out)-8))
	return out, true, nil
}
