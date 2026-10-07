package intake

import (
	"testing"

	"github.com/metacubex/mihomo/component/cfir"
)

type protocol struct{ d cfir.ProtocolDescriptor }
func (p protocol) Descriptor() cfir.ProtocolDescriptor { return p.d }

type backend struct{ d cfir.BackendDescriptor }
func (b backend) Descriptor() cfir.BackendDescriptor { return b.d }

type layer struct{ d cfir.LayerDescriptor }
func (l layer) Descriptor() cfir.LayerDescriptor { return l.d }

func TestSemanticDiffIgnoresSourceLayoutAndFindsCapabilityChange(t *testing.T) {
	oldBundle := cfir.ProviderBundle{
		Source: cfir.UpstreamSource{Project: "mihomo-next", Revision: "r1"},
		Layers: []cfir.ProtocolLayer{layer{d: cfir.LayerDescriptor{
			ID: "quic", Kind: cfir.LayerTransport, MinCFIR: cfir.CurrentVersion,
			Capabilities: cfir.NewCapabilitySet(cfir.CapabilityPathMigration),
		}}},
		Protocols: []cfir.ProtocolAdapter{protocol{d: cfir.ProtocolDescriptor{
			ID: "foo", MinCFIR: cfir.CurrentVersion,
			Primitives: cfir.PrimitiveSet(cfir.PrimitiveSession),
			Composition: []cfir.LayerRef{{ID: "quic", Kind: cfir.LayerTransport}},
		}}},
	}
	newBundle := cfir.ProviderBundle{
		Source: cfir.UpstreamSource{Project: "mihomo-next", Revision: "r2"},
		Layers: []cfir.ProtocolLayer{layer{d: cfir.LayerDescriptor{
			ID: "quic", Kind: cfir.LayerTransport, MinCFIR: cfir.CurrentVersion,
			Capabilities: cfir.NewCapabilitySet(
				cfir.CapabilityPathMigration,
				cfir.CapabilityPMTUFeedback,
			),
		}}},
		Protocols: []cfir.ProtocolAdapter{protocol{d: cfir.ProtocolDescriptor{
			ID: "foo", MinCFIR: cfir.CurrentVersion,
			Primitives: cfir.PrimitiveSet(cfir.PrimitiveSession),
			Composition: []cfir.LayerRef{{ID: "quic", Kind: cfir.LayerTransport}},
		}}},
		Backends: []cfir.Backend{backend{d: cfir.BackendDescriptor{
			ID: "next-stack", Kind: cfir.BackendTUN,
			Platforms: cfir.Platforms(cfir.PlatformAndroid, cfir.PlatformLinux),
			MinCFIR: cfir.CurrentVersion,
		}}},
	}

	before, err := Snapshot(oldBundle)
	if err != nil { t.Fatal(err) }
	after, err := Snapshot(newBundle)
	if err != nil { t.Fatal(err) }

	diff := Compare(before, after)
	if len(diff.Changes) != 2 {
		t.Fatalf("changes=%#v want layer change + backend add", diff.Changes)
	}
	if diff.Changes[0].Type != "backend" || diff.Changes[0].Kind != ChangeAdded {
		t.Fatalf("first change=%#v", diff.Changes[0])
	}
	if diff.Changes[1].Type != "layer" || diff.Changes[1].Kind != ChangeChanged {
		t.Fatalf("second change=%#v", diff.Changes[1])
	}
}

func TestSemanticDiffReportsProtocolRemoval(t *testing.T) {
	before := Manifest{
		Source: cfir.UpstreamSource{Project: "x", Revision: "1"},
		Protocols: map[cfir.ProtocolID]cfir.ProtocolDescriptor{
			"legacy": {ID: "legacy", MinCFIR: cfir.CurrentVersion, Primitives: cfir.PrimitiveSet(cfir.PrimitiveStream)},
		},
		Backends: map[string]cfir.BackendDescriptor{},
		Layers: map[cfir.LayerID]cfir.LayerDescriptor{},
	}
	after := Manifest{
		Source: cfir.UpstreamSource{Project: "x", Revision: "2"},
		Protocols: map[cfir.ProtocolID]cfir.ProtocolDescriptor{},
		Backends: map[string]cfir.BackendDescriptor{},
		Layers: map[cfir.LayerID]cfir.LayerDescriptor{},
	}
	diff := Compare(before, after)
	if len(diff.Changes) != 1 || diff.Changes[0].Kind != ChangeRemoved || diff.Changes[0].ID != "legacy" {
		t.Fatalf("unexpected removal diff: %#v", diff.Changes)
	}
}
