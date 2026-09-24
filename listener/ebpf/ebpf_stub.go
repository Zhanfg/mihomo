//go:build !with_ebpf || (!linux && !android)

package ebpf

import (
	"errors"

	C "github.com/metacubex/mihomo/constant"
	LC "github.com/metacubex/mihomo/listener/config"
)

type Listener struct {
	config LC.EBPF
}

func New(config LC.EBPF, _ C.Tunnel) (*Listener, error) {
	if config.Enable {
		return nil, errors.New("eBPF routing support is not included in this build; rebuild with -tags with_ebpf")
	}
	return &Listener{config: config}, nil
}

func (l *Listener) Config() LC.EBPF { return l.config }
func (l *Listener) Address() string { return "" }
func (l *Listener) RawAddress() string { return "" }
func (l *Listener) Close() error { return nil }
