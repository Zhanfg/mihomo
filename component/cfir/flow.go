package cfir

import (
	"errors"
	"net/netip"
)

type Endpoint struct {
	Addr   netip.Addr
	Domain string
	Port   uint16
}

func (e Endpoint) Valid() bool {
	return e.Port != 0 && (e.Addr.IsValid() || e.Domain != "")
}

type LatencyClass uint8

const (
	LatencyUnspecified LatencyClass = iota
	LatencyInteractive
	LatencyBalanced
	LatencyBulk
)

type ReliabilityClass uint8

const (
	ReliabilityUnspecified ReliabilityClass = iota
	ReliabilityBestEffort
	ReliabilityReliable
	ReliabilityCritical
)

type FlowIntent struct {
	Latency     LatencyClass
	Reliability ReliabilityClass
	Background  bool
}

type Generation struct {
	Network uint64
	Path    uint64
	Socket  uint64
}

// Flow is the protocol-neutral unit consumed by routing, Smart, telemetry and
// platform backends. Payload bytes deliberately do not live here; implementations
// pass buffer references/streams beside this descriptor so CFIR itself never
// forces an encode/decode or copy.
type Flow struct {
	Primitive    Primitive
	Source       Endpoint
	Destination  Endpoint
	Intent       FlowIntent
	Capabilities CapabilitySet
	Security     SecurityContext
	Generation   Generation
	Extensions   ExtensionBag
}

func (f Flow) Validate() error {
	if !f.Primitive.Valid() {
		return errors.New("cfir: invalid primitive")
	}
	if !f.Destination.Valid() {
		return errors.New("cfir: invalid destination")
	}
	if err := f.Security.Profile.Validate(); err != nil {
		return err
	}
	return nil
}
