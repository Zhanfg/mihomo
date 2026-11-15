//go:build with_ebpf && (linux || android)

package ebpf

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	CiliumEBPF "github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
)

func TestProcessCgroupExclusive(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "cgroup.procs")
	pid := strconv.Itoa(os.Getpid())
	if err := os.WriteFile(path, []byte(pid+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	exclusive, err := processCgroupExclusive(directory)
	if err != nil || !exclusive {
		t.Fatalf("single-process cgroup was not exclusive: exclusive=%v err=%v", exclusive, err)
	}
	if err = os.WriteFile(path, []byte(pid+"\n"+strconv.Itoa(os.Getpid()+1)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	exclusive, err = processCgroupExclusive(directory)
	if err != nil || exclusive {
		t.Fatalf("shared cgroup was treated as exclusive: exclusive=%v err=%v", exclusive, err)
	}
}

func TestProcessCgroupExclusiveRejectsPopulatedDescendant(t *testing.T) {
	directory := t.TempDir()
	pid := strconv.Itoa(os.Getpid())
	if err := os.WriteFile(filepath.Join(directory, "cgroup.procs"), []byte(pid+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(directory, "child")
	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(child, "cgroup.procs"), []byte(strconv.Itoa(os.Getpid()+1)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	exclusive, err := processCgroupExclusive(directory)
	if err != nil || exclusive {
		t.Fatalf("populated descendant was treated as exclusive: exclusive=%v err=%v", exclusive, err)
	}
}

func TestSelfBypassInstructionsUseSocketCookie(t *testing.T) {
	for name, instructions := range map[string]asm.Instructions{
		"create":      selfBypassCreateInstructions(1),
		"release":     selfBypassReleaseInstructions(1),
		"socket_addr": selfBypassSocketAddrInstructions(1),
	} {
		found := false
		for _, instruction := range instructions {
			if instruction.IsBuiltinCall() && asm.BuiltinFunc(instruction.Constant) == asm.FnGetSocketCookie {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("%s self-bypass instructions do not read the socket cookie", name)
		}
	}
}

func TestSelfBypassSocketAddrHooks(t *testing.T) {
	hooks := selfBypassSocketAddrHooks(SelfBypassCgroupConfig{
		EnableTCP:  true,
		EnableUDP:  true,
		EnableIPv6: true,
	})
	if len(hooks) != 4 {
		t.Fatalf("unexpected dual-stack self-bypass hook count: %d", len(hooks))
	}
	if hooks[0].attachType != CiliumEBPF.AttachCGroupInet4Connect ||
		hooks[1].attachType != CiliumEBPF.AttachCGroupInet6Connect ||
		hooks[2].attachType != CiliumEBPF.AttachCGroupUDP4Sendmsg ||
		hooks[3].attachType != CiliumEBPF.AttachCGroupUDP6Sendmsg {
		t.Fatalf("unexpected self-bypass hooks: %+v", hooks)
	}
	ipv4Only := selfBypassSocketAddrHooks(SelfBypassCgroupConfig{EnableTCP: true})
	if len(ipv4Only) != 1 || ipv4Only[0].attachType != CiliumEBPF.AttachCGroupInet4Connect {
		t.Fatalf("unexpected IPv4-only self-bypass hooks: %+v", ipv4Only)
	}
}

func TestSelfBypassFailedDialCookieRemoval(t *testing.T) {
	owner, err := NewSelfBypass()
	if err != nil {
		t.Skipf("eBPF map capability unavailable: %v", err)
	}
	defer owner.Close()

	socket, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Skipf("local socket unavailable: %v", err)
	}
	defer socket.Close()
	raw, err := socket.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	cookie, err := owner.RegisterSocketWithCookie(raw)
	if err != nil {
		t.Fatalf("register socket cookie: %v", err)
	}
	if cookie == 0 {
		t.Fatal("userspace socket registration returned no cookie")
	}
	var value uint32
	if err := owner.Map().Lookup(&cookie, &value); err != nil || value != 1 {
		t.Fatalf("expected live cookie entry, value=%d err=%v", value, err)
	}
	owner.UnregisterSocketCookie(cookie)
	if err := owner.Map().Lookup(&cookie, &value); err == nil {
		t.Fatal("failed dial cookie survived explicit cleanup")
	}
	owner.UnregisterSocketCookie(cookie) // safe and idempotent
}
