package adapter

import (
	"testing"

	"github.com/metacubex/mihomo/adapter/outbound"
	"github.com/metacubex/mihomo/common/structure"
)

type smuxCloseProbe struct {
	*outbound.Base
	closes int
}

func (p *smuxCloseProbe) Close() error {
	p.closes++
	return nil
}

func TestWrapProxyWithSingMuxOwnership(t *testing.T) {
	decoder := structure.NewDecoder(structure.Option{
		TagName: "proxy", WeaklyTypedInput: true, KeyReplacer: structure.DefaultKeyReplacer,
	})
	for _, tc := range []struct {
		name    string
		mapping map[string]any
		wantErr bool
		wrapped bool
	}{
		{"decode error", map[string]any{"enabled": []string{"invalid"}}, true, false},
		{"constructor error", map[string]any{"enabled": true, "protocol": "invalid-smux-protocol"}, true, false},
		{"disabled", map[string]any{"enabled": false}, false, false},
		{"enabled", map[string]any{"enabled": true, "protocol": "yamux"}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probe := &smuxCloseProbe{Base: outbound.NewBase(outbound.BaseOption{})}
			got, err := wrapProxyWithSingMux(probe, tc.mapping, decoder)
			if tc.wantErr {
				if err == nil || got != nil {
					t.Fatalf("expected nil adapter and error, got %v, %v", got, err)
				}
				if probe.closes != 1 {
					t.Fatalf("failed setup closed underlying adapter %d times, want 1", probe.closes)
				}
				return
			}
			if err != nil || got == nil {
				t.Fatalf("unexpected setup result: %v, %v", got, err)
			}
			if probe.closes != 0 {
				t.Fatalf("successful setup closed underlying adapter early")
			}
			if tc.wrapped {
				if _, ok := got.(*outbound.SingMux); !ok {
					t.Fatalf("expected SingMux, got %T", got)
				}
			} else if got != probe {
				t.Fatalf("disabled mux must return original adapter")
			}
			if err := got.Close(); err != nil {
				t.Fatal(err)
			}
			if probe.closes != 1 {
				t.Fatalf("closing successful adapter closed underlying adapter %d times, want 1", probe.closes)
			}
		})
	}
}
