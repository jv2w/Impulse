package world

import (
	"encoding/binary"
	"fmt"
	"slices"

	"github.com/segmentio/fasthash/fnv1a"
)

// networkBlockHash produces the canonical "network block hash" for a (name, properties) block state.
// This hash is used for mapping network palette entries back to runtime IDs.
//
// The scratch slice is used to reduce allocations. The returned slice should be passed back in on subsequent calls to
// reuse the same backing array.
func networkBlockHash(name string, properties map[string]any, scratch []byte) (uint32, []byte) {
	if name == "minecraft:unknown" {
		return 0xfffffffe, scratch // -2
	}

	keys := make([]string, 0, len(properties))
	for k := range properties {
		keys = append(keys, k)
	}
	// slices.Sort replaces sort.Strings: both put distinct strings in ascending order, so the key order and the
	// bytes hashed are the same, without boxing the slice into a sort.Interface.
	slices.Sort(keys)

	data := scratch[:0]
	// writeString became appendNetworkString, a plain function taking and returning the buffer. The closure used
	// to capture data by reference, which forced the slice header and the closure onto the heap on every call.
	// The bytes appended, and their order, are unchanged.

	data = append(data, 10) // compound
	data = append(data, 0)
	data = append(data, 0)

	data = append(data, 8) // string
	data = appendNetworkString(data, "name")
	data = appendNetworkString(data, name)

	data = append(data, 10) // compound
	data = appendNetworkString(data, "states")
	for _, k := range keys {
		v := properties[k]
		switch v := v.(type) {
		case string:
			data = append(data, 8) // string
			data = appendNetworkString(data, k)
			data = appendNetworkString(data, v)

		case uint8:
			data = append(data, 1) // tagByte
			data = appendNetworkString(data, k)
			data = append(data, byte(v))
		case int8:
			data = append(data, 1) // tagByte
			data = appendNetworkString(data, k)
			data = append(data, byte(v))
		case bool:
			b := 0
			if v {
				b = 1
			}
			data = append(data, 1) // tagByte
			data = appendNetworkString(data, k)
			data = append(data, byte(b))

		case uint16:
			data = append(data, 2) // tagInt16
			data = appendNetworkString(data, k)
			data = binary.LittleEndian.AppendUint16(data, uint16(v))
		case int16:
			data = append(data, 2) // tagInt16
			data = appendNetworkString(data, k)
			data = binary.LittleEndian.AppendUint16(data, uint16(v))

		case uint32:
			data = append(data, 3) // tagInt32
			data = appendNetworkString(data, k)
			data = binary.LittleEndian.AppendUint32(data, uint32(v))
		case int32:
			data = append(data, 3) // tagInt32
			data = appendNetworkString(data, k)
			data = binary.LittleEndian.AppendUint32(data, uint32(v))
		default:
			panic(fmt.Sprintf("unhandled nbt type: %T", v))
		}
	}
	data = append(data, 0) // end
	data = append(data, 0) // end

	return fnv1a.HashBytes32(data), data
}

// appendNetworkString appends str to data prefixed with its little endian uint16 length, as the network block hash
// format writes strings. It returns the grown buffer, which the caller must keep.
func appendNetworkString(data []byte, str string) []byte {
	data = binary.LittleEndian.AppendUint16(data, uint16(len(str)))
	return append(data, str...)
}
