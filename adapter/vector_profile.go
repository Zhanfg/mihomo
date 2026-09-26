package adapter

import (
	"math"
	"net/netip"
	"strings"

	C "github.com/metacubex/mihomo/constant"
)

// NodeVectorSize is deliberately small. A profile is evaluated only after
// Smart has already narrowed the candidate set; a fixed float32 vector is
// cheaper than maintaining a heavyweight ANN index for tens/hundreds of
// proxies and keeps Android heap/GC pressure predictable.
const NodeVectorSize = 12

const (
	vectorHealth = iota
	vectorReliability
	vectorLatency
	vectorStability
	vectorIPv4
	vectorIPv6
	vectorFamilyCountryConsistency
	vectorFamilyNetworkConsistency
	vectorUDP
	vectorTFO
	vectorMPTCP
	vectorLearnedTrust
)

// NodeVectorProfile is a compact learned/runtime view of one concrete proxy.
//
// Categorical metadata stays outside the dense vector: protocol/provider/
// country/ASN/vendor are compared exactly instead of wasting one-hot vector
// dimensions or accepting hash collisions. The numeric vector holds only
// comparable qualities used by the Smart reranker.
type NodeVectorProfile struct {
	Vector [NodeVectorSize]float32

	Protocol string
	Provider string

	IPv4Exit netip.Addr
	IPv6Exit netip.Addr

	Country4 string
	Country6 string
	ASN4     string
	ASN6     string
	Vendor4  string
	Vendor6  string
}

func clamp01(v float64) float32 {
	switch {
	case v <= 0:
		return 0
	case v >= 1:
		return 1
	default:
		return float32(v)
	}
}

func capabilityValue(known, ok bool) float32 {
	if !known {
		return 0.5
	}
	if ok {
		return 1
	}
	return 0
}

func healthVector(p C.Proxy, testURL string) (reliability, latency, stability float32) {
	if p == nil {
		return 0, 0, 0
	}
	history := p.DelayHistoryForTestUrl(testURL)
	if len(history) == 0 {
		if p.AliveForTestUrl(testURL) {
			return 0.5, 0.5, 0.5
		}
		return 0, 0, 0
	}

	successes := 0
	var sum, sumSq float64
	for _, item := range history {
		if item.Delay == 0 {
			continue
		}
		successes++
		d := float64(item.Delay)
		sum += d
		sumSq += d * d
	}

	// A small beta prior prevents one lucky health check from looking perfect.
	reliability = clamp01(float64(successes+2) / float64(len(history)+4))
	if successes == 0 {
		return reliability, 0, 0
	}

	mean := sum / float64(successes)
	latency = clamp01(math.Exp(-mean / 600.0))

	variance := sumSq/float64(successes) - mean*mean
	if variance < 0 {
		variance = 0
	}
	stddev := math.Sqrt(variance)
	cv := stddev / math.Max(mean, 1)
	stability = clamp01(1 / (1 + 2*cv))
	return
}

func familyConsistency(leftKnown bool, left string, rightKnown bool, right string) float32 {
	if !leftKnown || !rightKnown || left == "" || right == "" {
		return 0.5
	}
	if strings.EqualFold(left, right) {
		return 1
	}
	return 0
}

// BuildNodeVector projects existing Mihomo data into a compact vector. It does
// not own a second persistent database:
//
//   - family/exit/country telemetry: existing capability cache
//   - ASN/vendor: existing ASN.mmdb
//   - latency/jitter/alive: existing URL-test history
//   - contextual trust: existing Smart learned weight
//   - protocol/provider/TFO/MPTCP: proxy runtime metadata
//
// This avoids duplicating the large Smart history that is already persisted in
// bbolt. A restart rebuilds this tiny projection lazily from those sources.
func BuildNodeVector(p C.Proxy, testURL string, learnedWeight float64) NodeVectorProfile {
	var profile NodeVectorProfile
	if p == nil {
		return profile
	}

	profile.Protocol = p.Type().String()
	info := p.ProxyInfo()
	profile.Provider = info.ProviderName

	if p.AliveForTestUrl(testURL) {
		profile.Vector[vectorHealth] = 1
	}
	reliability, latency, stability := healthVector(p, testURL)
	profile.Vector[vectorReliability] = reliability
	profile.Vector[vectorLatency] = latency
	profile.Vector[vectorStability] = stability

	known4, ok4 := IPFamilyCapabilityKnown(p, false)
	known6, ok6 := IPFamilyCapabilityKnown(p, true)
	profile.Vector[vectorIPv4] = capabilityValue(known4, ok4)
	profile.Vector[vectorIPv6] = capabilityValue(known6, ok6)

	if known, ip := ExitIPForProxy(p, false); known {
		profile.IPv4Exit = ip
	}
	if known, ip := ExitIPForProxy(p, true); known {
		profile.IPv6Exit = ip
	}

	country4Known, country4 := ExitCountryForProxy(p, false)
	country6Known, country6 := ExitCountryForProxy(p, true)
	profile.Country4 = country4
	profile.Country6 = country6
	profile.Vector[vectorFamilyCountryConsistency] = familyConsistency(
		country4Known, profile.Country4, country6Known, profile.Country6)

	net4Known, asn4, vendor4 := ExitNetworkForProxy(p, false)
	net6Known, asn6, vendor6 := ExitNetworkForProxy(p, true)
	profile.ASN4, profile.Vendor4 = asn4, vendor4
	profile.ASN6, profile.Vendor6 = asn6, vendor6
	switch {
	case net4Known && net6Known && profile.ASN4 != "" && profile.ASN4 == profile.ASN6:
		profile.Vector[vectorFamilyNetworkConsistency] = 1
	case net4Known && net6Known && profile.Vendor4 != "" && strings.EqualFold(profile.Vendor4, profile.Vendor6):
		profile.Vector[vectorFamilyNetworkConsistency] = 0.85
	case !net4Known || !net6Known:
		profile.Vector[vectorFamilyNetworkConsistency] = 0.5
	default:
		profile.Vector[vectorFamilyNetworkConsistency] = 0
	}

	if known, ok := UDPCapabilityKnown(p); known {
		if ok {
			profile.Vector[vectorUDP] = 1
		}
	} else if p.SupportUDP() {
		// Config support is evidence, but weaker than an end-to-end STUN pass.
		profile.Vector[vectorUDP] = 0.6
	}

	if info.TFO {
		profile.Vector[vectorTFO] = 1
	}
	if info.MPTCP {
		profile.Vector[vectorMPTCP] = 1
	}

	if learnedWeight > 0 {
		// Smart's normal weights generally live around [0,1.25]. Saturate
		// rather than letting an outlier dominate every other dimension.
		profile.Vector[vectorLearnedTrust] = clamp01(learnedWeight / 1.25)
	} else {
		profile.Vector[vectorLearnedTrust] = 0.5
	}

	return profile
}

// NodeVectorScore is a second-stage Smart score in [0,1]. LightGBM / Smart's
// historical target weight remains the primary learner; this score injects
// orthogonal node facts that the legacy 30-feature model never sees.
//
// It intentionally performs O(NodeVectorSize) work on an already-filtered
// candidate. For normal provider sizes this is materially cheaper than keeping
// an ANN index and avoids another heap-resident vector database.
func NodeVectorScore(profile NodeVectorProfile, metadata *C.Metadata) float64 {
	weights := [NodeVectorSize]float32{
		0.12, // current health
		0.17, // observed reliability / credit
		0.14, // latency
		0.11, // jitter stability
		0.06, // IPv4 capability
		0.06, // IPv6 capability
		0.08, // v4/v6 country consistency
		0.04, // v4/v6 ASN/vendor consistency
		0.07, // UDP
		0.01, // TFO
		0.01, // MPTCP
		0.13, // existing Smart learned trust
	}

	// Make the query vector contextual without allocating another vector.
	if metadata != nil {
		if metadata.DstIP.IsValid() {
			if metadata.DstIP.Unmap().Is4() {
				weights[vectorIPv4] += 0.13
				weights[vectorIPv6] -= 0.04
			} else if metadata.DstIP.Is6() {
				weights[vectorIPv6] += 0.13
				weights[vectorIPv4] -= 0.04
			}
		}
		if metadata.NetWork == C.UDP {
			weights[vectorUDP] += 0.10
			weights[vectorLatency] += 0.03
			weights[vectorStability] += 0.03
		}
	}

	var weighted, total float64
	for i, value := range profile.Vector {
		w := weights[i]
		if w <= 0 {
			continue
		}
		weighted += float64(value * w)
		total += float64(w)
	}
	if total == 0 {
		return 0.5
	}
	score := weighted / total
	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}

// NodeVectorMatch is the allocation-free convenience path used by Smart.
func NodeVectorMatch(p C.Proxy, testURL string, metadata *C.Metadata, learnedWeight float64) float64 {
	return NodeVectorScore(BuildNodeVector(p, testURL, learnedWeight), metadata)
}
