package session

import (
	"testing"

	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// BenchmarkParseEntityMetadata measures the metadata build that runs once per
// viewer whenever an entity's state is sent.
func BenchmarkParseEntityMetadata(b *testing.B) {
	s := &Session{}
	e := &stubEntity{handle: world.NewEntity(stubEntityType{
		name: "impulse:plain",
		bbox: cube.Box(-0.3, 0, -0.3, 0.3, 1.8, 0.3),
	}, stubEntityConf{})}

	var m protocol.EntityMetadata
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		m = s.parseEntityMetadata(e)
	}
	b.StopTimer()

	if len(m) == 0 {
		b.Fatal("metadata is empty")
	}
}
