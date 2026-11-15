//go:build windows

package memory

import (
    "runtime"
    "testing"
    "unsafe"
)

// Windows PROCESS_MEMORY_COUNTERS uses pointer-sized SIZE_T fields.
// The previous uint64 layout was incorrect for 32-bit Windows.
func TestProcessMemoryCountersWindowsABI(t *testing.T) {
    want := uintptr(8) + 8*unsafe.Sizeof(uintptr(0))
    got := unsafe.Sizeof(PROCESS_MEMORY_COUNTERS{})
    if got != want {
        t.Fatalf("%s PROCESS_MEMORY_COUNTERS ABI size: got %d, want %d", runtime.GOARCH, got, want)
    }
    if unsafe.Offsetof(PROCESS_MEMORY_COUNTERS{}.WorkingSetSize) != 8+unsafe.Sizeof(uintptr(0)) {
        t.Fatal("WorkingSetSize offset does not match Windows layout")
    }
}
