package outbound

import (
	"strings"

	"github.com/metacubex/mihomo/component/cfir"
)

func cfirCaps(standard []cfir.StandardCapability, extensions ...string) cfir.CapabilitySet {
	capabilities := cfir.NewCapabilitySet(standard...)
	for _, extension := range extensions {
		if extension == "" {
			continue
		}
		if id, err := cfir.ParseExtensionID(extension); err == nil {
			_ = capabilities.AddExtension(id)
		}
	}
	return capabilities
}

func cfirProjection(
	id cfir.ProtocolID,
	displayName string,
	instance string,
	familyPrimitives cfir.PrimitiveMask,
	instancePrimitives cfir.PrimitiveMask,
	familyCapabilities cfir.CapabilitySet,
	instanceCapabilities cfir.CapabilitySet,
	familySecurity cfir.SecurityProfile,
	securityOffer cfir.SecurityProfile,
) cfir.ProtocolProjection {
	return cfir.ProtocolProjection{
		Family: cfir.ProtocolDescriptor{
			ID:           id,
			DisplayName:  displayName,
			MinCFIR:      cfir.CurrentVersion,
			Primitives:   familyPrimitives,
			Capabilities: familyCapabilities,
			Security:     familySecurity,
		},
		Instance: cfir.ProtocolCandidate{
			Protocol:     id,
			Instance:     instance,
			Primitives:    instancePrimitives,
			Capabilities:  instanceCapabilities,
			SecurityOffer: securityOffer,
			// Configuration describes intended security, not completed
			// negotiation. Runtime attestation is deliberately left false.
			Security: cfir.SecurityContext{},
		},
	}
}

func cfirTLSOffer(serverAuthenticated bool, forwardSecrecy bool) cfir.SecurityProfile {
	authentication := cfir.AuthenticationNone
	if serverAuthenticated {
		authentication = cfir.AuthenticationServer
	}
	return cfir.SecurityProfile{
		Authentication:  authentication,
		Confidentiality: cfir.ConfidentialityTransport,
		Integrity:       cfir.IntegrityAuthenticated,
		ForwardSecrecy:  forwardSecrecy,
	}
}

var cfirWireGuardSecurity = cfir.SecurityProfile{
	Authentication:   cfir.AuthenticationMutual,
	Confidentiality:  cfir.ConfidentialityEndToEnd,
	Integrity:        cfir.IntegrityAuthenticated,
	ForwardSecrecy:   true,
	ReplayProtection: true,
}

func (h *Hysteria2) CFIRProtocolProjection() cfir.ProtocolProjection {
	familyExtensions := []string{
		"hysteria2.transport.quic",
		"hysteria2.security.tls13-configured",
		"hysteria2.port-hopping",
		"hysteria2.obfs.salamander",
		"hysteria2.obfs.gecko",
		"hysteria2.realm",
	}
	familyCaps := cfirCaps([]cfir.StandardCapability{
		cfir.CapabilityMultiplex,
		cfir.CapabilityUnreliableDatagram,
		cfir.CapabilityCongestionControl,
	}, familyExtensions...)

	instanceExtensions := []string{
		"hysteria2.transport.quic",
		"hysteria2.security.tls13-configured",
	}
	if h.option != nil {
		if h.option.Ports != "" {
			instanceExtensions = append(instanceExtensions, "hysteria2.port-hopping")
		}
		switch strings.ToLower(h.option.Obfs) {
		case "salamander":
			instanceExtensions = append(instanceExtensions, "hysteria2.obfs.salamander")
		case "gecko":
			instanceExtensions = append(instanceExtensions, "hysteria2.obfs.gecko")
		}
		if h.option.RealmOpts.Enable {
			instanceExtensions = append(instanceExtensions, "hysteria2.realm")
		}
	}
	instanceCaps := cfirCaps([]cfir.StandardCapability{
		cfir.CapabilityMultiplex,
		cfir.CapabilityUnreliableDatagram,
		cfir.CapabilityCongestionControl,
	}, instanceExtensions...)

	primitives := cfir.PrimitiveSet(
		cfir.PrimitiveStream,
		cfir.PrimitiveDatagram,
		cfir.PrimitiveSession,
	)
	serverAuthenticated := h.option != nil && !h.option.SkipCertVerify
	familySecurity := cfirTLSOffer(false, false)
	securityOffer := cfirTLSOffer(serverAuthenticated, true)
	return cfirProjection(
		"hysteria2", "Hysteria2", h.Name(),
		primitives, primitives,
		familyCaps, instanceCaps,
		familySecurity, securityOffer,
	)
}

func (v *Vless) CFIRProtocolProjection() cfir.ProtocolProjection {
	familyExtensions := []string{
		"vless.udp",
		"vless.xudp",
		"vless.packet-addr",
		"vless.transport.tcp",
		"vless.transport.ws",
		"vless.transport.h2",
		"vless.transport.grpc",
		"vless.transport.xhttp",
		"vless.security.tls-configured",
		"vless.security.reality-configured",
		"vless.security.shadowtls-configured",
		"vless.security.restls-configured",
		"vless.security.jls-configured",
		"vless.encryption.configured",
	}
	familyCaps := cfirCaps(
		[]cfir.StandardCapability{cfir.CapabilityPacketAddress},
		familyExtensions...,
	)

	instancePrimitives := cfir.PrimitiveSet(cfir.PrimitiveStream)
	instanceStandard := make([]cfir.StandardCapability, 0, 1)
	instanceExtensions := make([]string, 0, 8)
	securityOffer := cfir.SecurityProfile{}
	if v.SupportUDP() {
		instancePrimitives |= cfir.PrimitiveSet(cfir.PrimitiveDatagram)
		instanceExtensions = append(instanceExtensions, "vless.udp")
	}
	if v.option != nil {
		if v.option.PacketAddr {
			instanceStandard = append(instanceStandard, cfir.CapabilityPacketAddress)
			instanceExtensions = append(instanceExtensions, "vless.packet-addr")
		}
		if v.option.XUDP {
			instanceExtensions = append(instanceExtensions, "vless.xudp")
		}
		network := strings.ToLower(v.option.Network)
		if network == "" {
			network = "tcp"
		}
		switch network {
		case "ws":
			instanceExtensions = append(instanceExtensions, "vless.transport.ws")
		case "h2":
			instanceExtensions = append(instanceExtensions, "vless.transport.h2")
		case "grpc":
			instanceExtensions = append(instanceExtensions, "vless.transport.grpc")
		case "xhttp":
			instanceExtensions = append(instanceExtensions, "vless.transport.xhttp")
		default:
			instanceExtensions = append(instanceExtensions, "vless.transport.tcp")
		}
		if v.option.TLS {
			instanceExtensions = append(instanceExtensions, "vless.security.tls-configured")
			securityOffer = cfirTLSOffer(!v.option.SkipCertVerify, false)
		}
		if v.realityConfig != nil {
			instanceExtensions = append(instanceExtensions, "vless.security.reality-configured")
		}
		if v.shadowTLSConfig != nil {
			instanceExtensions = append(instanceExtensions, "vless.security.shadowtls-configured")
		}
		if v.restlsConfig != nil {
			instanceExtensions = append(instanceExtensions, "vless.security.restls-configured")
		}
		if v.jlsConfig != nil {
			instanceExtensions = append(instanceExtensions, "vless.security.jls-configured")
		}
		if v.option.Encryption != "" {
			instanceExtensions = append(instanceExtensions, "vless.encryption.configured")
		}
	}

	return cfirProjection(
		"vless", "VLESS", v.Name(),
		cfir.PrimitiveSet(cfir.PrimitiveStream, cfir.PrimitiveDatagram),
		instancePrimitives,
		familyCaps,
		cfirCaps(instanceStandard, instanceExtensions...),
		cfir.SecurityProfile{}, securityOffer,
	)
}

func (w *WireGuard) CFIRProtocolProjection() cfir.ProtocolProjection {
	familyExtensions := []string{
		"wireguard.noise-configured",
		"wireguard.ip-stack.auto",
		"wireguard.ip-stack.mips",
		"wireguard.ip-stack.gvisor",
		"wireguard.amnezia",
		"wireguard.multi-peer",
		"wireguard.remote-dns",
	}
	familyCaps := cfirCaps([]cfir.StandardCapability{
		cfir.CapabilityCongestionControl,
		cfir.CapabilityNativeResolve,
	}, familyExtensions...)

	instancePrimitives := cfir.PrimitiveSet(cfir.PrimitiveStream, cfir.PrimitivePacket)
	if w.SupportUDP() {
		instancePrimitives |= cfir.PrimitiveSet(cfir.PrimitiveDatagram)
	}
	instanceStandard := make([]cfir.StandardCapability, 0, 2)
	instanceExtensions := []string{"wireguard.noise-configured"}

	stackMode := strings.ToLower(w.option.IPStack.Mode)
	if stackMode == "" {
		stackMode = ipStackAuto
	}
	switch stackMode {
	case ipStackMips:
		instanceExtensions = append(instanceExtensions, "wireguard.ip-stack.mips")
	case ipStackGVisor:
		instanceExtensions = append(instanceExtensions, "wireguard.ip-stack.gvisor")
	default:
		instanceExtensions = append(instanceExtensions, "wireguard.ip-stack.auto")
	}
	if w.option.IPStack.CongestionController != "" {
		instanceStandard = append(instanceStandard, cfir.CapabilityCongestionControl)
	}
	if w.option.AmneziaWGOption != nil {
		instanceExtensions = append(instanceExtensions, "wireguard.amnezia")
	}
	if len(w.option.Peers) > 1 {
		instanceExtensions = append(instanceExtensions, "wireguard.multi-peer")
	}
	if w.option.RemoteDnsResolve && len(w.option.Dns) > 0 {
		instanceStandard = append(instanceStandard, cfir.CapabilityNativeResolve)
		instanceExtensions = append(instanceExtensions, "wireguard.remote-dns")
	}

	return cfirProjection(
		"wireguard", "WireGuard", w.Name(),
		cfir.PrimitiveSet(cfir.PrimitiveStream, cfir.PrimitiveDatagram, cfir.PrimitivePacket),
		instancePrimitives,
		familyCaps,
		cfirCaps(instanceStandard, instanceExtensions...),
		cfirWireGuardSecurity, cfirWireGuardSecurity,
	)
}

func (m *Masque) CFIRProtocolProjection() cfir.ProtocolProjection {
	familyExtensions := []string{
		"masque.transport.h2",
		"masque.transport.h3",
		"masque.mode.h3-l4proxy",
		"masque.security.tls-configured",
		"masque.ip-stack.auto",
		"masque.ip-stack.mips",
		"masque.ip-stack.gvisor",
		"masque.remote-dns",
	}
	familyCaps := cfirCaps([]cfir.StandardCapability{
		cfir.CapabilityMultiplex,
		cfir.CapabilityCongestionControl,
		cfir.CapabilityNativeResolve,
	}, familyExtensions...)

	instancePrimitives := cfir.PrimitiveSet(
		cfir.PrimitiveStream,
		cfir.PrimitivePacket,
		cfir.PrimitiveSession,
	)
	if m.SupportUDP() {
		instancePrimitives |= cfir.PrimitiveSet(cfir.PrimitiveDatagram)
	}
	instanceStandard := []cfir.StandardCapability{cfir.CapabilityMultiplex}
	instanceExtensions := []string{"masque.security.tls-configured"}
	securityOffer := cfirTLSOffer(!m.option.SkipCertVerify, false)

	switch strings.ToLower(m.option.Network) {
	case "h2":
		instanceExtensions = append(instanceExtensions, "masque.transport.h2")
	case "h3-l4proxy":
		instanceExtensions = append(instanceExtensions, "masque.transport.h3", "masque.mode.h3-l4proxy")
	default:
		instanceExtensions = append(instanceExtensions, "masque.transport.h3")
	}
	stackMode := strings.ToLower(m.option.IPStack.Mode)
	if stackMode == "" {
		stackMode = ipStackAuto
	}
	switch stackMode {
	case ipStackMips:
		instanceExtensions = append(instanceExtensions, "masque.ip-stack.mips")
	case ipStackGVisor:
		instanceExtensions = append(instanceExtensions, "masque.ip-stack.gvisor")
	default:
		instanceExtensions = append(instanceExtensions, "masque.ip-stack.auto")
	}
	if strings.ToLower(m.option.Network) != "h2" && m.option.CongestionController != "" {
		instanceStandard = append(instanceStandard, cfir.CapabilityCongestionControl)
	}
	if m.option.RemoteDnsResolve && len(m.option.Dns) > 0 {
		instanceStandard = append(instanceStandard, cfir.CapabilityNativeResolve)
		instanceExtensions = append(instanceExtensions, "masque.remote-dns")
	}

	return cfirProjection(
		"masque", "MASQUE", m.Name(),
		cfir.PrimitiveSet(
			cfir.PrimitiveStream,
			cfir.PrimitiveDatagram,
			cfir.PrimitivePacket,
			cfir.PrimitiveSession,
		),
		instancePrimitives,
		familyCaps,
		cfirCaps(instanceStandard, instanceExtensions...),
		cfirTLSOffer(false, false), securityOffer,
	)
}

func cfirAttestedSecurity(offer cfir.SecurityProfile, peerIdentity string) cfir.SecurityContext {
	if offer == (cfir.SecurityProfile{}) {
		return cfir.SecurityContext{}
	}
	return cfir.SecurityContext{
		Profile:        offer,
		PeerIdentity:   peerIdentity,
		AttestedByCore: true,
	}
}

func (h *Hysteria2) cfirSecurityEvidence() cfir.SecurityContext {
	peer := ""
	if h.option != nil && !h.option.SkipCertVerify {
		peer = h.option.NameCertVerify
		if peer == "" {
			peer = h.option.SNI
		}
		if peer == "" {
			peer = h.option.Server
		}
	}
	return cfirAttestedSecurity(h.CFIRProtocolProjection().Instance.SecurityOffer, peer)
}

func (v *Vless) cfirSecurityEvidence() cfir.SecurityContext {
	peer := ""
	if v.option != nil && v.option.TLS && !v.option.SkipCertVerify {
		peer = v.option.NameCertVerify
		if peer == "" {
			peer = v.option.ServerName
		}
		if peer == "" {
			peer = v.option.Server
		}
	}
	return cfirAttestedSecurity(v.CFIRProtocolProjection().Instance.SecurityOffer, peer)
}

func (w *WireGuard) cfirSecurityEvidence() cfir.SecurityContext {
	return cfirAttestedSecurity(w.CFIRProtocolProjection().Instance.SecurityOffer, "")
}

func (m *Masque) cfirSecurityEvidence() cfir.SecurityContext {
	peer := ""
	if !m.option.SkipCertVerify {
		peer = m.option.SNI
		if peer == "" {
			peer = m.option.Server
		}
	}
	return cfirAttestedSecurity(m.CFIRProtocolProjection().Instance.SecurityOffer, peer)
}

var (
	_ cfir.ProtocolProjectionProvider = (*Hysteria2)(nil)
	_ cfir.ProtocolProjectionProvider = (*Vless)(nil)
	_ cfir.ProtocolProjectionProvider = (*WireGuard)(nil)
	_ cfir.ProtocolProjectionProvider = (*Masque)(nil)
)
