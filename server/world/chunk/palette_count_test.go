package chunk

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// hugePaletteCount is an entry count no legal chunk can carry: 50 million entries is far past the 1<<16
// ceiling of the largest palette size. Sized to allocate roughly 190 MiB as a []uint32, which is well clear
// of the maxPaletteAlloc budget the tests below assert against.
const hugePaletteCount = 50_000_000

// maxPaletteAlloc is the amount decoding a malformed palette header may allocate. The header itself is a
// handful of bytes, so anything approaching hugePaletteCount*4 means the count was believed and the slice
// was made before the stream ran out of entries.
const maxPaletteAlloc = 16 << 20

// allocatedBy reports the number of bytes allocated by f. TotalAlloc is cumulative and unaffected by
// collection, so a slice that is made and then immediately dropped still shows up here.
func allocatedBy(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// TestDecodePaletteRejectsUnaddressableCount feeds the disk encoding a palette header declaring far more
// entries than blockSize bits can index, with no entry data behind it. The decoder used to trust the
// count and make the slice first, turning a handful of bytes from a corrupt world file (mcdb DiskDecode)
// into a heap allocation of hundreds of megabytes.
//
// Asserting only on the error is not enough: the pre-fix decoders returned an error too, after allocating.
// The allocation bound is the part that distinguishes the two.
func TestDecodePaletteRejectsUnaddressableCount(t *testing.T) {
	diskPayload := func() *bytes.Buffer {
		buf := &bytes.Buffer{}
		_ = binary.Write(buf, binary.LittleEndian, uint32(hugePaletteCount))
		return buf
	}

	for _, tc := range []struct {
		name string
		e    Encoding
		buf  func() *bytes.Buffer
	}{
		{name: "disk", e: DiskEncoding, buf: diskPayload},
	} {
		t.Run(tc.name, func(t *testing.T) {
			buf := tc.buf()
			if buf.Len() > 16 {
				t.Fatalf("payload = %d bytes, want a header sized payload", buf.Len())
			}

			var err error
			allocated := allocatedBy(func() {
				_, err = tc.e.decodePalette(buf, 1, BiomePaletteEncoding)
			})
			if err == nil {
				t.Fatalf("decodePalette err = nil, want the entry count to be rejected")
			}
			if !strings.Contains(err.Error(), "palette entry count") {
				t.Errorf("error = %q, want it to mention the entry count", err.Error())
			}
			if allocated > maxPaletteAlloc {
				t.Errorf("decodePalette allocated %d bytes, want at most %d: the count was believed before it was checked", allocated, maxPaletteAlloc)
			}
		})
	}
}

// TestDecodePaletteAcceptsFullPalette pins the other side of the bound: a palette holding exactly the
// 1<<blockSize entries the indices can address still decodes, for every size the encoder emits. Size 0 is
// the special case where the count is not read from the stream at all and is fixed at 1.
func TestDecodePaletteAcceptsFullPalette(t *testing.T) {
	for _, size := range sizes {
		if size > 8 {
			// A 16 bit palette holds 65536 entries; the smaller sizes already cover the boundary and
			// this keeps the test payloads small.
			continue
		}
		t.Run(fmt.Sprintf("size %d", size), func(t *testing.T) {
			count := 1 << size
			if size == 0 {
				count = 1
			}

			disk := &bytes.Buffer{}
			network := &bytes.Buffer{}
			if size != 0 {
				_ = binary.Write(disk, binary.LittleEndian, uint32(count))
				_ = protocol.WriteVarint32(network, int32(count))
			}
			for i := range count {
				BiomePaletteEncoding.encode(disk, uint32(i))
				_ = protocol.WriteVarint32(network, int32(i))
			}

			for _, tc := range []struct {
				name string
				e    Encoding
				buf  *bytes.Buffer
			}{
				{name: "disk", e: DiskEncoding, buf: disk},
				{name: "network", e: NetworkEncoding, buf: network},
			} {
				t.Run(tc.name, func(t *testing.T) {
					p, err := tc.e.decodePalette(tc.buf, size, BiomePaletteEncoding)
					assertNoError(t, err, "decodePalette(size=%d)", size)
					if p.Len() != count {
						t.Fatalf("palette length = %d, want %d", p.Len(), count)
					}
					for i := range count {
						if got := p.Value(uint16(i)); got != uint32(i) {
							t.Errorf("palette value %d = %d, want %d", i, got, i)
						}
					}
				})
			}
		})
	}
}

// TestDecodePaletteRejectsOneOverFullPalette is the tightest failing case: a single entry more than the
// indices can reach. The entries are present in the payload, so nothing but the count check can turn it
// away.
func TestDecodePaletteRejectsOneOverFullPalette(t *testing.T) {
	for _, size := range []paletteSize{1, 2, 3, 4, 5, 6, 8} {
		t.Run(fmt.Sprintf("size %d", size), func(t *testing.T) {
			count := (1 << size) + 1

			disk := &bytes.Buffer{}
			_ = binary.Write(disk, binary.LittleEndian, uint32(count))
			for i := range count {
				BiomePaletteEncoding.encode(disk, uint32(i))
			}

			for _, tc := range []struct {
				name string
				e    Encoding
				buf  *bytes.Buffer
			}{
				{name: "disk", e: DiskEncoding, buf: disk},
			} {
				t.Run(tc.name, func(t *testing.T) {
					if _, err := tc.e.decodePalette(tc.buf, size, BiomePaletteEncoding); err == nil {
						t.Fatalf("decodePalette err = nil, want %d entries to be rejected for a %d bit palette", count, size)
					}
				})
			}
		})
	}
}
