package legacybridge

import (
	"errors"
	"net"
	"net/netip"

	"github.com/metacubex/mihomo/component/cfir"
	C "github.com/metacubex/mihomo/constant"
)

const MetadataExtensionID cfir.ExtensionID = "mihomo.legacy.metadata"

type MetadataExtension struct {
	Type           C.Type
	DNSMode        C.DNSMode
	InIP           netip.Addr
	InPort         uint16
	InName         string
	InUser         string
	RematchName    string
	UUID           string
	RawSrcAddr     net.Addr
	RawDstAddr     net.Addr
	UID            uint32
	Process        string
	ProcessPath    string
	SpecialProxy   string
	SpecialRules   string
	RemoteDst      string
	DSCP           uint8
	SniffHost      string
	SrcIPASN       string
	DstIPASN       string
	SrcGeoIP       []string
	DstGeoIP       []string
	SmartBlock     string
	SmartTarget    string
	WildcardTarget string
}

func (m MetadataExtension) CFIRExtensionID() cfir.ExtensionID {
	return MetadataExtensionID
}

func cloneStringsPreserveNil(values []string) []string {
	if values == nil {
		return nil
	}
	out := make([]string, len(values))
	copy(out, values)
	return out
}

func primitiveFromNetwork(network C.NetWork) (cfir.Primitive, error) {
	switch network {
	case C.TCP:
		return cfir.PrimitiveStream, nil
	case C.UDP:
		return cfir.PrimitiveDatagram, nil
	default:
		return cfir.PrimitiveInvalid, errors.New("cfir legacy bridge: unsupported metadata network")
	}
}

// FromMetadata is intentionally side-effect free. It creates a CFIR descriptor
// beside the legacy flow; existing routing and protocol code remains the source
// of truth until differential tests prove the bridge lossless enough to migrate.
func FromMetadata(metadata *C.Metadata, generation cfir.Generation) (cfir.Flow, error) {
	if metadata == nil {
		return cfir.Flow{}, errors.New("cfir legacy bridge: nil metadata")
	}
	primitive, err := primitiveFromNetwork(metadata.NetWork)
	if err != nil {
		return cfir.Flow{}, err
	}

	flow := cfir.Flow{
		Primitive: primitive,
		Source: cfir.Endpoint{
			Addr: metadata.SrcIP,
			Port: metadata.SrcPort,
		},
		Destination: cfir.Endpoint{
			Addr:   metadata.DstIP,
			Domain: metadata.Host,
			Port:   metadata.DstPort,
		},
		Generation: generation,
		Security: cfir.SecurityContext{
			// The bridge cannot infer negotiated wire security from legacy
			// metadata, so it must never upgrade the security claim.
			Profile: cfir.SecurityProfile{
				Authentication:  cfir.AuthenticationUnknown,
				Confidentiality: cfir.ConfidentialityUnknown,
				Integrity:       cfir.IntegrityUnknown,
			},
			AttestedByCore: false,
		},
		Extensions: cfir.ExtensionBag{
			MetadataExtension{
				Type:           metadata.Type,
				DNSMode:        metadata.DNSMode,
				InIP:           metadata.InIP,
				InPort:         metadata.InPort,
				InName:         metadata.InName,
				InUser:         metadata.InUser,
				RematchName:    metadata.RematchName,
				UUID:           metadata.UUID,
				RawSrcAddr:     metadata.RawSrcAddr,
				RawDstAddr:     metadata.RawDstAddr,
				UID:            metadata.Uid,
				Process:        metadata.Process,
				ProcessPath:    metadata.ProcessPath,
				SpecialProxy:   metadata.SpecialProxy,
				SpecialRules:   metadata.SpecialRules,
				RemoteDst:      metadata.RemoteDst,
				DSCP:           metadata.DSCP,
				SniffHost:      metadata.SniffHost,
				SrcIPASN:       metadata.SrcIPASN,
				DstIPASN:       metadata.DstIPASN,
				SrcGeoIP:       cloneStringsPreserveNil(metadata.SrcGeoIP),
				DstGeoIP:       cloneStringsPreserveNil(metadata.DstGeoIP),
				SmartBlock:     metadata.SmartBlock,
				SmartTarget:    metadata.SmartTarget,
				WildcardTarget: metadata.WildcardTarget,
			},
		},
	}
	if err := flow.Validate(); err != nil {
		return cfir.Flow{}, err
	}
	return flow, nil
}

func metadataExtension(flow cfir.Flow) (MetadataExtension, bool) {
	extension, ok := flow.Extensions.Find(MetadataExtensionID)
	if !ok {
		return MetadataExtension{}, false
	}
	switch value := extension.(type) {
	case MetadataExtension:
		return value, true
	case *MetadataExtension:
		if value != nil {
			return *value, true
		}
	}
	return MetadataExtension{}, false
}

// ToMetadata supports shadow/differential testing and staged migration. It
// reconstructs legacy metadata without teaching CFIR about Mihomo-only fields.
func ToMetadata(flow cfir.Flow) (*C.Metadata, error) {
	if err := flow.Validate(); err != nil {
		return nil, err
	}

	metadata := &C.Metadata{
		SrcIP:   flow.Source.Addr,
		SrcPort: flow.Source.Port,
		DstIP:   flow.Destination.Addr,
		Host:    flow.Destination.Domain,
		DstPort: flow.Destination.Port,
	}
	switch flow.Primitive {
	case cfir.PrimitiveStream:
		metadata.NetWork = C.TCP
	case cfir.PrimitiveDatagram:
		metadata.NetWork = C.UDP
	default:
		return nil, errors.New("cfir legacy bridge: primitive has no legacy metadata network")
	}

	if extension, ok := metadataExtension(flow); ok {
		metadata.Type = extension.Type
		metadata.DNSMode = extension.DNSMode
		metadata.InIP = extension.InIP
		metadata.InPort = extension.InPort
		metadata.InName = extension.InName
		metadata.InUser = extension.InUser
		metadata.RematchName = extension.RematchName
		metadata.UUID = extension.UUID
		metadata.RawSrcAddr = extension.RawSrcAddr
		metadata.RawDstAddr = extension.RawDstAddr
		metadata.Uid = extension.UID
		metadata.Process = extension.Process
		metadata.ProcessPath = extension.ProcessPath
		metadata.SpecialProxy = extension.SpecialProxy
		metadata.SpecialRules = extension.SpecialRules
		metadata.RemoteDst = extension.RemoteDst
		metadata.DSCP = extension.DSCP
		metadata.SniffHost = extension.SniffHost
		metadata.SrcIPASN = extension.SrcIPASN
		metadata.DstIPASN = extension.DstIPASN
		metadata.SrcGeoIP = cloneStringsPreserveNil(extension.SrcGeoIP)
		metadata.DstGeoIP = cloneStringsPreserveNil(extension.DstGeoIP)
		metadata.SmartBlock = extension.SmartBlock
		metadata.SmartTarget = extension.SmartTarget
		metadata.WildcardTarget = extension.WildcardTarget
	}
	return metadata, nil
}
