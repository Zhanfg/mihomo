package adapter

import (
	"encoding/binary"
	"net/netip"
	"testing"
)

func stunSuccessWithAttr(t *testing.T, txid []byte, attrType uint16, value []byte) []byte {
	t.Helper()
	if len(txid) != 12 {
		t.Fatal("txid must be 12 bytes")
	}
	padded := (len(value) + 3) &^ 3
	msg := make([]byte, 20+4+padded)
	binary.BigEndian.PutUint16(msg[0:2], 0x0101)
	binary.BigEndian.PutUint16(msg[2:4], uint16(len(msg)-20))
	binary.BigEndian.PutUint32(msg[4:8], 0x2112A442)
	copy(msg[8:20], txid)
	binary.BigEndian.PutUint16(msg[20:22], attrType)
	binary.BigEndian.PutUint16(msg[22:24], uint16(len(value)))
	copy(msg[24:24+len(value)], value)
	return msg
}

func TestSTUNXORMappedIPv4(t *testing.T) {
	txid := []byte{1,2,3,4,5,6,7,8,9,10,11,12}
	want := netip.MustParseAddr("203.0.113.9")
	raw := want.As4()
	cookie := [4]byte{0x21,0x12,0xA4,0x42}
	value := make([]byte, 8)
	value[1] = 0x01
	binary.BigEndian.PutUint16(value[2:4], 443^0x2112)
	for i := range raw {
		value[4+i] = raw[i]^cookie[i]
	}
	msg := stunSuccessWithAttr(t, txid, 0x0020, value)
	got, ok := stunMappedAddress(msg, txid)
	if !ok || got != want {
		t.Fatalf("mapped=%v ok=%v want=%v", got, ok, want)
	}
}

func TestSTUNXORMappedIPv6(t *testing.T) {
	txid := []byte{12,11,10,9,8,7,6,5,4,3,2,1}
	want := netip.MustParseAddr("2001:db8::1234")
	raw := want.As16()
	mask := [16]byte{0x21,0x12,0xA4,0x42}
	copy(mask[4:], txid)
	value := make([]byte, 20)
	value[1] = 0x02
	for i := range raw {
		value[4+i] = raw[i]^mask[i]
	}
	msg := stunSuccessWithAttr(t, txid, 0x0020, value)
	got, ok := stunMappedAddress(msg, txid)
	if !ok || got != want {
		t.Fatalf("mapped=%v ok=%v want=%v", got, ok, want)
	}
}

func TestSTUNMappedIPv4(t *testing.T) {
	txid := []byte{1,1,1,1,1,1,1,1,1,1,1,1}
	want := netip.MustParseAddr("198.51.100.7")
	raw := want.As4()
	value := make([]byte, 8)
	value[1] = 0x01
	copy(value[4:8], raw[:])
	msg := stunSuccessWithAttr(t, txid, 0x0001, value)
	got, ok := stunMappedAddress(msg, txid)
	if !ok || got != want {
		t.Fatalf("mapped=%v ok=%v want=%v", got, ok, want)
	}
}

func TestSTUNMappedRejectsMalformedAttribute(t *testing.T) {
	txid := []byte{1,2,3,4,5,6,7,8,9,10,11,12}
	msg := make([]byte, 24)
	binary.BigEndian.PutUint16(msg[0:2], 0x0101)
	binary.BigEndian.PutUint16(msg[2:4], 4)
	binary.BigEndian.PutUint32(msg[4:8], 0x2112A442)
	copy(msg[8:20], txid)
	binary.BigEndian.PutUint16(msg[20:22], 0x0020)
	binary.BigEndian.PutUint16(msg[22:24], 8)
	if _, ok := stunMappedAddress(msg, txid); ok {
		t.Fatal("truncated attribute must be rejected")
	}
}
