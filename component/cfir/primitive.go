package cfir

// Primitive is the smallest transport semantic the core understands. Protocol
// names intentionally do not appear here.
type Primitive uint8

const (
	PrimitiveInvalid Primitive = iota
	PrimitiveStream
	PrimitiveDatagram
	PrimitivePacket
	PrimitiveSession
)

func (p Primitive) Valid() bool {
	return p >= PrimitiveStream && p <= PrimitiveSession
}

func (p Primitive) String() string {
	switch p {
	case PrimitiveStream:
		return "stream"
	case PrimitiveDatagram:
		return "datagram"
	case PrimitivePacket:
		return "packet"
	case PrimitiveSession:
		return "session"
	default:
		return "invalid"
	}
}

type PrimitiveMask uint8

func PrimitiveSet(primitives ...Primitive) PrimitiveMask {
	var mask PrimitiveMask
	for _, primitive := range primitives {
		if primitive.Valid() {
			mask |= 1 << (primitive - 1)
		}
	}
	return mask
}

func (m PrimitiveMask) Supports(primitive Primitive) bool {
	if !primitive.Valid() {
		return false
	}
	return m&(1<<(primitive-1)) != 0
}
