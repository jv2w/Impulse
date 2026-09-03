package block

import (
	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
	"time"
)

// Log is a naturally occurring block found in trees, primarily used to create planks. It comes in six
// species: oak, spruce, birch, jungle, acacia, and dark oak.
// Stripped log is a variant obtained by using an axe on a log.
type Log struct {
	solid
	bass

	// Wood is the type of wood of the log. This field must have one of the values found in the material
	// package.
	Wood WoodType
	// Stripped specifies if the log is stripped or not.
	Stripped bool
	// Axis is the axis which the log block faces.
	Axis cube.Axis
}

// FlammabilityInfo ...
func (l Log) FlammabilityInfo() FlammabilityInfo {
	if !l.Wood.Flammable() {
		return newFlammabilityInfo(0, 0, false)
	}
	return newFlammabilityInfo(5, 5, true)
}

// BreakInfo ...
func (l Log) BreakInfo() BreakInfo {
	breakInfo := newBreakInfo(2, alwaysHarvestable, axeEffective, oneOf(l))
	if l.Wood == MangroveWood() && !l.Stripped {
		// Mangrove logs have a much lower blast resistance than all other logs.
		return breakInfo.withBlastResistance(0.4)
	}
	return breakInfo
}

// SmeltInfo ...
func (Log) SmeltInfo() item.SmeltInfo {
	return newSmeltInfo(item.NewStack(item.Charcoal{}, 1), 0.15)
}

// FuelInfo ...
func (l Log) FuelInfo() item.FuelInfo {
	if !l.Wood.Flammable() {
		return item.FuelInfo{}
	}
	return newFuelInfo(time.Second * 15)
}

// UseOnBlock handles the rotational placing of logs.
func (l Log) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) (used bool) {
	pos, face, used = firstReplaceable(tx, pos, face, l)
	if !used {
		return
	}
	l.Axis = face.Axis()

	place(tx, pos, l, user, ctx)
	return placed(ctx)
}

// Strip ...
func (l Log) Strip() (world.Block, world.Sound, bool) {
	return Log{Axis: l.Axis, Wood: l.Wood, Stripped: true}, nil, !l.Stripped
}

// logName gives the encoded name of a log of a wood type.
var logName = enumNamer(WoodTypes(), func(w WoodType) string {
	switch w {
	case CrimsonWood(), WarpedWood():
		return w.String() + "_stem"
	default:
		return w.String() + "_log"
	}
})

// strippedLogName gives the encoded name of a stripped log of a wood type.
var strippedLogName = enumNamer(WoodTypes(), func(w WoodType) string {
	switch w {
	case CrimsonWood(), WarpedWood():
		return "stripped_" + w.String() + "_stem"
	default:
		return "stripped_" + w.String() + "_log"
	}
})

// EncodeItem ...
func (l Log) EncodeItem() (name string, meta int16) {
	// enumNamer prepends "minecraft:" to the same suffix the switch built and
	// falls back to building it for a wood type outside the list, so the name is
	// unchanged while the registered types only cost a lookup.
	if !l.Stripped {
		return logName(l.Wood), 0
	}
	return strippedLogName(l.Wood), 0
}

// EncodeBlock ...
func (l Log) EncodeBlock() (name string, properties map[string]any) {
	if !l.Stripped {
		return logName(l.Wood), map[string]any{"pillar_axis": l.Axis.String()}
	}
	return strippedLogName(l.Wood), map[string]any{"pillar_axis": l.Axis.String()}
}

// allLogs returns a list of all possible log states.
func allLogs() (logs []world.Block) {
	// Every wood type but bamboo gives three axes times a stripped and an
	// unstripped log, so the final length is known before the loop.
	woods := WoodTypes()
	logs = make([]world.Block, 0, (len(woods)-1)*6)
	for _, w := range woods {
		if w == BambooWood() {
			continue
		}
		for axis := cube.Axis(0); axis < 3; axis++ {
			logs = append(logs, Log{Axis: axis, Stripped: true, Wood: w})
			logs = append(logs, Log{Axis: axis, Stripped: false, Wood: w})
		}
	}
	return
}
