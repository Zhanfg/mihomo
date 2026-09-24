//go:build with_ebpf && (linux || android)

package ebpf

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strconv"
	"sync"
	"syscall"
	"time"

	singebpf "github.com/CHIZI-0618/sing-ebpf"
	ebpfruntime "github.com/CHIZI-0618/sing-ebpf/runtime"
	"github.com/metacubex/mihomo/adapter/inbound"
	"github.com/metacubex/mihomo/common/pool"
	"github.com/metacubex/mihomo/component/dialer"
	"github.com/metacubex/mihomo/component/iface"
	"github.com/metacubex/mihomo/component/keepalive"
	C "github.com/metacubex/mihomo/constant"
	LC "github.com/metacubex/mihomo/listener/config"
	"github.com/metacubex/mihomo/log"
	"github.com/metacubex/mihomo/transport/socks5"
	"github.com/sagernet/netlink"
	"golang.org/x/sys/unix"
)

const (
	routeEventDebounce       = 250 * time.Millisecond
	routeFallbackInterval    = 30 * time.Second
	routePollFallbackInterval = 2 * time.Second
)

type Listener struct {
	config LC.EBPF
	tunnel C.Tunnel

	selfBypass *singebpf.SelfBypass
	backend    *singebpf.TCBackend
	runtime    ebpfruntime.TCRuntime

	tcp4 net.Listener
	tcp6 net.Listener
	udp4 *net.UDPConn
	udp6 *net.UDPConn
	port uint16

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu            sync.RWMutex
	interfaceName string
	closed        bool
}

func New(config LC.EBPF, tunnel C.Tunnel) (*Listener, error) {
	if !config.Enable {
		return nil, errors.New("eBPF listener is disabled")
	}
	if !config.AutoDetectInterface && config.Interface == "" && dialer.DefaultInterface.Load() == "" {
		return nil, errors.New("eBPF interface-name is empty and auto-detect-interface is disabled")
	}

	ctx, cancel := context.WithCancel(context.Background())
	l := &Listener{
		config: config,
		tunnel: tunnel,
		ctx:    ctx,
		cancel: cancel,
	}

	if err := l.start(); err != nil {
		_ = l.Close()
		return nil, err
	}
	return l, nil
}

func (l *Listener) Config() LC.EBPF {
	return l.config
}

func (l *Listener) RawAddress() string {
	return l.Address()
}

func (l *Listener) Address() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return fmt.Sprintf("ebpf://%s:%d", l.interfaceName, l.port)
}

func (l *Listener) start() error {
	selfBypass, err := singebpf.NewSelfBypassWithCapacity(singebpf.CompactSelfBypassSocketCapacity)
	if err != nil {
		return fmt.Errorf("create eBPF self-bypass map: %w", err)
	}
	l.selfBypass = selfBypass

	// Register every Mihomo-owned outbound socket by SO_COOKIE. This avoids
	// relying on Android UID or fwmark semantics and prevents self interception.
	dialer.SetAdditionalSocketHook(func(_, _ string, raw syscall.RawConn) error {
		return l.selfBypass.RegisterSocket(raw)
	})

	if err = l.startInternalListeners(); err != nil {
		return err
	}

	policy, err := singebpf.CompilePolicy(singebpf.PolicyConfig{
		EnableTCP: true,
		EnableUDP: true,
		Local: singebpf.LocalPolicy{
			DNSMode:              singebpf.DNSModeHijack,
			BypassPrivateAddress: l.config.BypassPrivate,
		},
		SharedDNSMode: singebpf.DNSModeOff,
	})
	if err != nil {
		return fmt.Errorf("compile eBPF policy: %w", err)
	}

	backend, err := singebpf.PrepareTC(singebpf.TCConfig{
		ListenerPort:       l.port,
		EnableLocal:        true,
		EnableShared:       false,
		EnableIPv4:         true,
		EnableLocalIPv6:    l.config.IPv6,
		EnableSharedIPv6:   false,
		EnableTCP:          true,
		EnableUDP:          true,
		Policy:             policy,
		SelfBypass:         l.selfBypass,
		AssignmentCapacity: singebpf.CompactTCAssignmentCapacity,
	})
	if err != nil {
		return fmt.Errorf("prepare eBPF TC backend: %w", err)
	}
	l.backend = backend

	if err = l.registerTCPListeners(); err != nil {
		return err
	}

	interfaceName, err := l.resolveInterface()
	if err != nil {
		return err
	}
	hostAddresses := collectHostAddresses()

	rt, err := ebpfruntime.NewTCRuntime(ebpfruntime.TCRuntimeConfig{
		Backend:          backend,
		LocalEnabled:     true,
		IPv6Enabled:      l.config.IPv6,
		LocalInterface:   interfaceName,
		HostAddresses:    hostAddresses,
		Priority:         l.config.TCPriority,
		SharedInterfaces: nil,
	})
	if err != nil {
		return fmt.Errorf("start eBPF TC runtime: %w", err)
	}
	l.runtime = rt
	l.setInterfaceName(interfaceName)

	if err = backend.Enable(); err != nil {
		return fmt.Errorf("enable eBPF TC backend: %w", err)
	}

	info := rt.NetworkInfo()
	log.Infoln("[EBPF] routing active: interface=%s listener=%d delivery=%s mark=%#x table=%d priority=%d",
		interfaceName, l.port, info.DeliveryInterface, info.RoutingMark, info.RoutingTable, info.RoutingPriority)

	if l.config.AutoDetectInterface && l.config.Interface == "" && dialer.DefaultInterface.Load() == "" {
		l.wg.Add(1)
		go l.monitorInterface()
	}
	return nil
}

func (l *Listener) startInternalListeners() error {
	tcp4, err := l.listenTCP("tcp4", "0.0.0.0:0", false)
	if err != nil {
		return fmt.Errorf("listen eBPF TCP4: %w", err)
	}
	l.tcp4 = tcp4
	_, portText, err := net.SplitHostPort(tcp4.Addr().String())
	if err != nil {
		return err
	}
	port64, err := strconv.ParseUint(portText, 10, 16)
	if err != nil {
		return err
	}
	l.port = uint16(port64)

	udp4, err := l.listenUDP("udp4", net.JoinHostPort("0.0.0.0", portText), false)
	if err != nil {
		return fmt.Errorf("listen eBPF UDP4: %w", err)
	}
	l.udp4 = udp4

	if l.config.IPv6 {
		tcp6, err := l.listenTCP("tcp6", net.JoinHostPort("::", portText), true)
		if err != nil {
			return fmt.Errorf("listen eBPF TCP6: %w", err)
		}
		l.tcp6 = tcp6
		udp6, err := l.listenUDP("udp6", net.JoinHostPort("::", portText), true)
		if err != nil {
			return fmt.Errorf("listen eBPF UDP6: %w", err)
		}
		l.udp6 = udp6
	}

	l.wg.Add(1)
	go l.acceptTCP(l.tcp4)
	l.wg.Add(1)
	go l.readUDP(l.udp4)
	if l.tcp6 != nil {
		l.wg.Add(1)
		go l.acceptTCP(l.tcp6)
	}
	if l.udp6 != nil {
		l.wg.Add(1)
		go l.readUDP(l.udp6)
	}
	return nil
}

func (l *Listener) socketControl(ipv6, udp bool) func(string, string, syscall.RawConn) error {
	return func(_, _ string, raw syscall.RawConn) error {
		var controlErr error
		if err := raw.Control(func(fd uintptr) {
			if err := unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1); err != nil {
				controlErr = err
				return
			}
			if ipv6 {
				if err := unix.SetsockoptInt(int(fd), unix.SOL_IPV6, unix.IPV6_TRANSPARENT, 1); err != nil {
					controlErr = err
					return
				}
				if err := unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_V6ONLY, 1); err != nil {
					controlErr = err
					return
				}
				if udp {
					if err := unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_RECVPKTINFO, 1); err != nil {
						controlErr = err
						return
					}
					controlErr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_RECVORIGDSTADDR, 1)
				}
			} else {
				if err := unix.SetsockoptInt(int(fd), unix.SOL_IP, unix.IP_TRANSPARENT, 1); err != nil {
					controlErr = err
					return
				}
				if udp {
					if err := unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_PKTINFO, 1); err != nil {
						controlErr = err
						return
					}
					controlErr = unix.SetsockoptInt(int(fd), unix.SOL_IP, unix.IP_RECVORIGDSTADDR, 1)
				}
			}
		}); err != nil {
			return err
		}
		if controlErr != nil {
			return controlErr
		}
		return l.selfBypass.RegisterSocket(raw)
	}
}

func (l *Listener) listenTCP(network, address string, ipv6 bool) (net.Listener, error) {
	lc := net.ListenConfig{Control: l.socketControl(ipv6, false)}
	return lc.Listen(l.ctx, network, address)
}

func (l *Listener) listenUDP(network, address string, ipv6 bool) (*net.UDPConn, error) {
	lc := net.ListenConfig{Control: l.socketControl(ipv6, true)}
	pc, err := lc.ListenPacket(l.ctx, network, address)
	if err != nil {
		return nil, err
	}
	udp, ok := pc.(*net.UDPConn)
	if !ok {
		_ = pc.Close()
		return nil, fmt.Errorf("unexpected eBPF UDP listener type %T", pc)
	}
	return udp, nil
}

func (l *Listener) registerTCPListeners() error {
	for _, item := range []struct {
		listener net.Listener
		ipv6     bool
	}{
		{l.tcp4, false},
		{l.tcp6, true},
	} {
		if item.listener == nil {
			continue
		}
		conn, ok := item.listener.(syscall.Conn)
		if !ok {
			return fmt.Errorf("eBPF TCP listener does not expose syscall.Conn")
		}
		raw, err := conn.SyscallConn()
		if err != nil {
			return err
		}
		var registerErr error
		if err = raw.Control(func(fd uintptr) {
			registerErr = l.backend.RegisterTCPListener(item.ipv6, int(fd))
		}); err != nil {
			return err
		}
		if registerErr != nil {
			return registerErr
		}
	}
	return nil
}

func (l *Listener) acceptTCP(listener net.Listener) {
	defer l.wg.Done()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if l.isClosed() {
				return
			}
			continue
		}
		go l.handleTCP(conn)
	}
}

func (l *Listener) handleTCP(conn net.Conn) {
	source, okSource := addrPort(conn.RemoteAddr())
	destination, okDestination := addrPort(conn.LocalAddr())
	if !okSource || !okDestination {
		_ = conn.Close()
		return
	}
	if l.backend == nil {
		_ = conn.Close()
		return
	}
	if _, err := l.backend.LookupAssignment(singebpf.ProtocolTCP, source, destination, 0, true); err != nil {
		log.Debugln("[EBPF] TCP assignment lookup failed %s -> %s: %v", source, destination, err)
		_ = conn.Close()
		return
	}
	keepalive.TCPKeepAlive(conn)
	target := socks5.AddrFromStdAddrPort(destination)
	l.tunnel.HandleTCPConn(inbound.NewSocket(
		target,
		conn,
		C.EBPF,
		inbound.WithInName("DEFAULT-EBPF"),
	))
}

func (l *Listener) readUDP(conn *net.UDPConn) {
	defer l.wg.Done()
	oob := make([]byte, 1024)
	for {
		buf := pool.Get(pool.UDPBufferSize)
		n, oobn, _, source, err := conn.ReadMsgUDPAddrPort(buf, oob)
		if err != nil {
			pool.Put(buf)
			if l.isClosed() {
				return
			}
			continue
		}
		destination, interfaceIndex, err := packetDestination(oob[:oobn])
		if err != nil {
			pool.Put(buf)
			continue
		}
		if l.backend == nil {
			pool.Put(buf)
			continue
		}
		_, err = l.backend.LookupAssignment(singebpf.ProtocolUDP, source, destination, interfaceIndex, false)
		if err != nil && interfaceIndex != 0 {
			_, err = l.backend.LookupAssignment(singebpf.ProtocolUDP, source, destination, 0, false)
		}
		if err != nil {
			pool.Put(buf)
			log.Debugln("[EBPF] UDP assignment lookup failed %s -> %s: %v", source, destination, err)
			continue
		}
		target := socks5.AddrFromStdAddrPort(destination)
		packet := udpPacketPool.Get().(*udpPacket)
		*packet = udpPacket{
			pc:         conn,
			client:     source,
			destination: destination,
			buf:        buf[:n],
			tunnel:     l.tunnel,
			selfBypass: l.selfBypass,
		}
		l.tunnel.HandleUDPPacket(inbound.NewPacket(
			target,
			packet,
			C.EBPF,
			inbound.WithInName("DEFAULT-EBPF"),
		))
	}
}

func packetDestination(oob []byte) (netip.AddrPort, uint32, error) {
	messages, err := unix.ParseSocketControlMessage(oob)
	if err != nil {
		return netip.AddrPort{}, 0, err
	}
	var destination netip.AddrPort
	var interfaceIndex uint32
	for index := range messages {
		message := &messages[index]
		if sockaddr, parseErr := unix.ParseOrigDstAddr(message); parseErr == nil {
			switch addr := sockaddr.(type) {
			case *unix.SockaddrInet4:
				destination = netip.AddrPortFrom(netip.AddrFrom4(addr.Addr), uint16(addr.Port))
			case *unix.SockaddrInet6:
				destination = netip.AddrPortFrom(netip.AddrFrom16(addr.Addr), uint16(addr.Port))
			}
		}
		switch {
		case message.Header.Level == unix.IPPROTO_IP && message.Header.Type == unix.IP_PKTINFO && len(message.Data) >= unix.SizeofInet4Pktinfo:
			interfaceIndex = binary.NativeEndian.Uint32(message.Data[:4])
		case message.Header.Level == unix.IPPROTO_IPV6 && message.Header.Type == unix.IPV6_PKTINFO && len(message.Data) >= unix.SizeofInet6Pktinfo:
			interfaceIndex = binary.NativeEndian.Uint32(message.Data[16:20])
		}
	}
	if !destination.IsValid() {
		return netip.AddrPort{}, interfaceIndex, errors.New("original destination is missing")
	}
	return destination, interfaceIndex, nil
}

func (l *Listener) resolveInterface() (string, error) {
	if l.config.Interface != "" {
		return l.config.Interface, nil
	}
	if configured := dialer.DefaultInterface.Load(); configured != "" {
		return configured, nil
	}
	if !l.config.AutoDetectInterface {
		return "", errors.New("no eBPF interface configured")
	}
	return detectDefaultInterface()
}

func detectDefaultInterface() (string, error) {
	for _, destination := range []net.IP{net.IPv4(1, 1, 1, 1), net.ParseIP("2606:4700:4700::1111")} {
		if destination == nil {
			continue
		}
		routes, err := netlink.RouteGet(destination)
		if err != nil {
			continue
		}
		for _, route := range routes {
			index := route.LinkIndex
			if index == 0 && len(route.MultiPath) > 0 && route.MultiPath[0] != nil {
				index = route.MultiPath[0].LinkIndex
			}
			if index == 0 {
				continue
			}
			link, err := netlink.LinkByIndex(index)
			if err != nil || link == nil || link.Attrs() == nil {
				continue
			}
			name := link.Attrs().Name
			if name != "" && name != "lo" {
				return name, nil
			}
		}
	}
	return "", errors.New("unable to detect default eBPF interface")
}

func collectHostAddresses() []netip.Addr {
	interfaces, err := iface.Interfaces()
	if err != nil {
		return nil
	}
	seen := make(map[netip.Addr]struct{})
	var result []netip.Addr
	for _, current := range interfaces {
		for _, prefix := range current.Addresses {
			address := prefix.Addr().Unmap()
			if !address.IsValid() {
				continue
			}
			if _, exists := seen[address]; exists {
				continue
			}
			seen[address] = struct{}{}
			result = append(result, address)
		}
	}
	return result
}

func (l *Listener) monitorInterface() {
	defer l.wg.Done()

	updates := make(chan netlink.RouteUpdate, 32)
	done := make(chan struct{})
	subscribed := netlink.RouteSubscribe(updates, done) == nil
	if subscribed {
		defer close(done)
	} else {
		log.Warnln("[EBPF] route subscription unavailable, falling back to polling")
	}

	fallbackInterval := routeFallbackInterval
	if !subscribed {
		fallbackInterval = routePollFallbackInterval
	}
	fallback := time.NewTicker(fallbackInterval)
	defer fallback.Stop()

	var debounce *time.Timer
	var debounceC <-chan time.Time
	armDebounce := func() {
		if debounce == nil {
			debounce = time.NewTimer(routeEventDebounce)
			debounceC = debounce.C
			return
		}
		if !debounce.Stop() {
			select {
			case <-debounce.C:
			default:
			}
		}
		debounce.Reset(routeEventDebounce)
		debounceC = debounce.C
	}

	for {
		select {
		case <-l.ctx.Done():
			if debounce != nil {
				debounce.Stop()
			}
			return
		case _, ok := <-updates:
			if !ok {
				updates = nil
				if subscribed {
					subscribed = false
					fallback.Reset(routePollFallbackInterval)
					log.Warnln("[EBPF] route subscription closed, falling back to polling")
				}
				continue
			}
			armDebounce()
		case <-debounceC:
			debounceC = nil
			l.reconcileInterface(true)
		case <-fallback.C:
			l.reconcileInterface(true)
		}
	}
}

func (l *Listener) reconcileInterface(refreshAddresses bool) {
	next, err := detectDefaultInterface()
	if err != nil {
		return
	}
	current := l.currentInterface()
	if next == current && !refreshAddresses {
		return
	}

	var addresses []netip.Addr
	if refreshAddresses || next != current {
		iface.FlushCache()
		addresses = collectHostAddresses()
	}
	if err = l.runtime.Reconcile(next, nil, addresses); err != nil {
		log.Warnln("[EBPF] interface reconcile failed: %v", err)
		return
	}
	if next != current {
		l.setInterfaceName(next)
		log.Infoln("[EBPF] default interface switched: %s -> %s", current, next)
	}
}

func (l *Listener) currentInterface() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.interfaceName
}

func (l *Listener) setInterfaceName(name string) {
	l.mu.Lock()
	l.interfaceName = name
	l.mu.Unlock()
}

func (l *Listener) isClosed() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.closed
}

func (l *Listener) Close() error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	l.mu.Unlock()

	if l.cancel != nil {
		l.cancel()
	}
	dialer.SetAdditionalSocketHook(nil)

	var errs []error
	if l.runtime != nil {
		if err := l.runtime.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	for _, closer := range []interface{ Close() error }{l.tcp4, l.tcp6, l.udp4, l.udp6} {
		if closer != nil {
			if err := closer.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
				errs = append(errs, err)
			}
		}
	}
	l.wg.Wait()
	if l.runtime == nil && l.backend != nil {
		if err := l.backend.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if l.selfBypass != nil {
		if err := l.selfBypass.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func addrPort(addr net.Addr) (netip.AddrPort, bool) {
	type addrPortProvider interface {
		AddrPort() netip.AddrPort
	}
	provider, ok := addr.(addrPortProvider)
	if !ok {
		return netip.AddrPort{}, false
	}
	value := provider.AddrPort()
	return value, value.IsValid()
}

var udpPacketPool = sync.Pool{
	New: func() any { return new(udpPacket) },
}

type udpPacket struct {
	pc          net.PacketConn
	client      netip.AddrPort
	destination netip.AddrPort
	buf         []byte
	tunnel      C.Tunnel
	selfBypass  *singebpf.SelfBypass
}

func (p *udpPacket) Data() []byte {
	return p.buf
}

func (p *udpPacket) LocalAddr() net.Addr {
	return net.UDPAddrFromAddrPort(p.client)
}

func (p *udpPacket) InAddr() net.Addr {
	return p.pc.LocalAddr()
}

func (p *udpPacket) Drop() {
	if p.buf != nil {
		pool.Put(p.buf)
	}
	*p = udpPacket{}
	udpPacketPool.Put(p)
}

func (p *udpPacket) WriteBack(data []byte, addr net.Addr) (int, error) {
	source := p.destination
	if addr != nil {
		if udpAddr, ok := addr.(*net.UDPAddr); ok {
			source = udpAddr.AddrPort()
		}
	}
	conn, err := p.createOrGetReplyConn(source)
	if err != nil {
		return 0, err
	}
	return conn.Write(data)
}

func (p *udpPacket) createOrGetReplyConn(source netip.AddrPort) (*net.UDPConn, error) {
	local := p.client.String()
	remote := source.String()
	natTable := p.tunnel.NatTable()
	if existing := natTable.GetForLocalConn(local, remote); existing != nil {
		return existing, nil
	}

	cond, loaded := natTable.GetOrCreateLockForLocalConn(local, remote)
	if loaded {
		cond.L.Lock()
		cond.Wait()
		conn := natTable.GetForLocalConn(local, remote)
		cond.L.Unlock()
		if conn == nil {
			return nil, errors.New("eBPF UDP reply NAT entry not found")
		}
		return conn, nil
	}
	if cond == nil {
		return nil, errors.New("eBPF UDP reply NAT lock not found")
	}
	defer func() {
		natTable.DeleteLockForLocalConn(local, remote)
		cond.Broadcast()
	}()

	conn, err := dialTransparentUDP(source, p.client, p.selfBypass)
	if err != nil {
		return nil, err
	}
	natTable.AddForLocalConn(local, remote, conn)
	return conn, nil
}

func dialTransparentUDP(source, destination netip.AddrPort, selfBypass *singebpf.SelfBypass) (*net.UDPConn, error) {
	family := unix.AF_INET6
	if source.Addr().Is4() && destination.Addr().Is4() {
		family = unix.AF_INET
	}
	fd, err := unix.Socket(family, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	closeFD := true
	defer func() {
		if closeFD {
			_ = unix.Close(fd)
		}
	}()

	if err = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_REUSEADDR, 1); err != nil {
		return nil, err
	}
	if family == unix.AF_INET {
		if err = unix.SetsockoptInt(fd, unix.SOL_IP, unix.IP_TRANSPARENT, 1); err != nil {
			return nil, err
		}
	} else {
		if err = unix.SetsockoptInt(fd, unix.SOL_IPV6, unix.IPV6_TRANSPARENT, 1); err != nil {
			return nil, err
		}
	}

	sourceSockaddr, err := sockaddr(source)
	if err != nil {
		return nil, err
	}
	destinationSockaddr, err := sockaddr(destination)
	if err != nil {
		return nil, err
	}
	if err = unix.Bind(fd, sourceSockaddr); err != nil {
		return nil, err
	}
	if err = unix.Connect(fd, destinationSockaddr); err != nil {
		return nil, err
	}

	file := os.NewFile(uintptr(fd), "mihomo-ebpf-udp-reply")
	if file == nil {
		return nil, errors.New("create eBPF UDP reply file")
	}
	closeFD = false
	defer file.Close()

	if selfBypass != nil {
		raw, rawErr := file.SyscallConn()
		if rawErr != nil {
			return nil, rawErr
		}
		if err = selfBypass.RegisterSocket(raw); err != nil {
			return nil, err
		}
	}

	conn, err := net.FileConn(file)
	if err != nil {
		return nil, err
	}
	udp, ok := conn.(*net.UDPConn)
	if !ok {
		_ = conn.Close()
		return nil, fmt.Errorf("unexpected UDP reply connection type %T", conn)
	}
	return udp, nil
}

func sockaddr(address netip.AddrPort) (unix.Sockaddr, error) {
	addr := address.Addr().Unmap()
	if addr.Is4() {
		return &unix.SockaddrInet4{Port: int(address.Port()), Addr: addr.As4()}, nil
	}
	if addr.Is6() {
		zoneID := uint32(0)
		if zone := addr.Zone(); zone != "" {
			if parsed, err := strconv.ParseUint(zone, 10, 32); err == nil {
				zoneID = uint32(parsed)
			}
		}
		return &unix.SockaddrInet6{Port: int(address.Port()), Addr: addr.As16(), ZoneId: zoneID}, nil
	}
	return nil, errors.New("invalid UDP address")
}
