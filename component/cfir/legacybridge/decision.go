package legacybridge

import (
	"errors"
	"fmt"

	"github.com/metacubex/mihomo/component/cfir"
	C "github.com/metacubex/mihomo/constant"
)

const maxProxyUnwrapDepth = 32

type LegacyDecision struct {
	Action        cfir.RouteAction
	LogicalTarget string
	LeafName      string
	LeafType      C.AdapterType
	Protocol      cfir.ProtocolID
	Candidate     cfir.ProtocolDescriptor
	Resolved      bool
	Depth         uint8
}

func RouteActionForAdapterType(adapterType C.AdapterType) cfir.RouteAction {
	switch adapterType {
	case C.Direct:
		return cfir.RouteActionDirect
	case C.Reject, C.RejectDrop:
		return cfir.RouteActionReject
	case C.Dns:
		return cfir.RouteActionDNS
	default:
		return cfir.RouteActionForward
	}
}

func ProtocolIDForAdapterType(adapterType C.AdapterType) (cfir.ProtocolID, bool) {
	switch adapterType {
	case C.Shadowsocks:
		return "shadowsocks", true
	case C.ShadowsocksR:
		return "shadowsocksr", true
	case C.Snell:
		return "snell", true
	case C.Socks5:
		return "socks5", true
	case C.Http:
		return "http-connect", true
	case C.Vmess:
		return "vmess", true
	case C.Vless:
		return "vless", true
	case C.Trojan:
		return "trojan", true
	case C.Hysteria:
		return "hysteria", true
	case C.Hysteria2:
		return "hysteria2", true
	case C.WireGuard:
		return "wireguard", true
	case C.Tuic:
		return "tuic", true
	case C.Ssh:
		return "ssh", true
	case C.Mieru:
		return "mieru", true
	case C.AnyTLS:
		return "anytls", true
	case C.Sudoku:
		return "sudoku", true
	case C.Masque:
		return "masque", true
	case C.TrustTunnel:
		return "trusttunnel", true
	case C.ShadowQuic:
		return "shadowquic", true
	case C.OpenVPN:
		return "openvpn", true
	case C.Tailscale:
		return "tailscale", true
	case C.ZeroTier:
		return "zerotier", true
	case C.EasyTier:
		return "easytier", true
	case C.GostRelay:
		return "gost-relay", true
	default:
		return "", false
	}
}

func primitiveForMetadata(metadata *C.Metadata) (cfir.Primitive, error) {
	if metadata == nil {
		return cfir.PrimitiveInvalid, errors.New("cfir legacy decision: nil metadata")
	}
	switch metadata.NetWork {
	case C.TCP:
		return cfir.PrimitiveStream, nil
	case C.UDP:
		return cfir.PrimitiveDatagram, nil
	default:
		return cfir.PrimitiveInvalid, fmt.Errorf("cfir legacy decision: unsupported network %v", metadata.NetWork)
	}
}

// DescriptorForLegacyLeaf conservatively describes only semantics that the
// legacy adapter API proves. It deliberately does not infer TLS/AEAD strength
// from an adapter name; runtime security must be attested separately.
func DescriptorForLegacyLeaf(adapterType C.AdapterType, supportUDP, supportUOT, l3 bool, info C.ProxyInfo) (cfir.ProtocolDescriptor, bool) {
	id, ok := ProtocolIDForAdapterType(adapterType)
	if !ok {
		return cfir.ProtocolDescriptor{}, false
	}

	primitives := cfir.PrimitiveSet(cfir.PrimitiveStream)
	if supportUDP {
		primitives |= cfir.PrimitiveSet(cfir.PrimitiveDatagram)
	}
	if l3 {
		primitives |= cfir.PrimitiveSet(cfir.PrimitivePacket)
	}

	capabilities := cfir.NewCapabilitySet()
	if info.SMUX {
		capabilities.AddStandard(cfir.CapabilityMultiplex)
	}
	if supportUOT {
		if extension, err := cfir.ParseExtensionID("mihomo.uot.reliable-datagram"); err == nil {
			_ = capabilities.AddExtension(extension)
		}
	}

	return cfir.ProtocolDescriptor{
		ID:           id,
		DisplayName:  adapterType.String(),
		MinCFIR:      cfir.CurrentVersion,
		Primitives:   primitives,
		Capabilities: capabilities,
		// Unknown is intentional: adapter type alone cannot prove negotiated
		// authentication, confidentiality or replay properties.
		Security: cfir.SecurityProfile{},
	}, true
}

// ProjectProxyDecision unwraps legacy selector/group layers without touching
// health state and projects the realized leaf into CFIR semantics.
func ProjectProxyDecision(proxy C.ProxyAdapter, metadata *C.Metadata) (LegacyDecision, error) {
	if proxy == nil {
		return LegacyDecision{}, errors.New("cfir legacy decision: nil proxy")
	}
	if metadata == nil {
		return LegacyDecision{}, errors.New("cfir legacy decision: nil metadata")
	}

	decision := LegacyDecision{LogicalTarget: proxy.Name()}
	current := proxy
	probeMetadata := metadata.Clone()

	for depth := 0; depth < maxProxyUnwrapDepth; depth++ {
		decision.Depth = uint8(depth)
		next := current.Unwrap(probeMetadata, false)
		if next == nil {
			break
		}
		if next.Name() == current.Name() && next.Type() == current.Type() {
			break
		}
		current = next
		if depth == maxProxyUnwrapDepth-1 {
			return LegacyDecision{}, errors.New("cfir legacy decision: proxy unwrap depth exceeded")
		}
	}

	decision.LeafName = current.Name()
	decision.LeafType = current.Type()
	decision.Action = RouteActionForAdapterType(current.Type())

	if decision.Action != cfir.RouteActionForward {
		decision.Resolved = true
		return decision, nil
	}

	descriptor, ok := DescriptorForLegacyLeaf(
		current.Type(),
		current.SupportUDP(),
		current.SupportUOT(),
		current.IsL3Protocol(probeMetadata),
		current.ProxyInfo(),
	)
	if !ok {
		// A future or control adapter is representable as an unresolved forward
		// target without teaching CFIR a protocol-name branch.
		return decision, nil
	}
	decision.Protocol = descriptor.ID
	decision.Candidate = descriptor
	decision.Resolved = true
	return decision, nil
}

func IntentForLegacyDecision(metadata *C.Metadata, generation cfir.Generation, action cfir.RouteAction) (cfir.ExecutionIntent, error) {
	primitive, err := primitiveForMetadata(metadata)
	if err != nil {
		return cfir.ExecutionIntent{}, err
	}
	intent := cfir.ExecutionIntent{
		Action:     action,
		Primitive:  primitive,
		Generation: generation,
	}
	if err := intent.Validate(); err != nil {
		return cfir.ExecutionIntent{}, err
	}
	return intent, nil
}
