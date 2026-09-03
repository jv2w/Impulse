package item

import (
	"github.com/jv2w/Impulse/server/world/sound"
)

// MusicDisc is an item that can be played in jukeboxes.
type MusicDisc struct {
	// DiscType is the disc type of the music disc.
	DiscType sound.DiscType
}

// MaxCount always returns 1.
func (MusicDisc) MaxCount() int {
	return 1
}

// musicDiscNames maps the name of every vanilla music disc to the encoded item
// name it forms, so that EncodeItem stops concatenating one on every call. A
// disc that is not in the table falls back to that concatenation.
var musicDiscNames = discNames()

// discNames builds the table above. DiscType.String returns a constant per
// disc, so one call feeds both the key and the value.
func discNames() map[string]string {
	discs := sound.MusicDiscs()
	names := make(map[string]string, len(discs))
	for _, d := range discs {
		name := d.String()
		names[name] = "minecraft:music_disc_" + name
	}
	return names
}

// EncodeItem ...
func (m MusicDisc) EncodeItem() (name string, meta int16) {
	// String is still called first, so an unknown disc panics exactly where it
	// used to.
	disc := m.DiscType.String()
	if n, ok := musicDiscNames[disc]; ok {
		return n, 0
	}
	return "minecraft:music_disc_" + disc, 0
}
