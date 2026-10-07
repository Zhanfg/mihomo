package tcpstats

import (
	"net"
	"reflect"
	"syscall"
)

const maxUnwrapSteps = 256

func getTCPStats(conn net.Conn) *Stats {
	var seenBuf [16]uintptr
	seen := seenBuf[:0]
outer:
	for depth := 0; depth < maxUnwrapSteps; depth++ {
		if rv := reflect.ValueOf(conn); rv.Kind() == reflect.Ptr {
			ptr := rv.Pointer()
			for _, prev := range seen {
				if prev == ptr {
					return nil
				}
			}
			seen = append(seen, ptr)
		}

		if sc, ok := conn.(interface {
			SyscallConn() (syscall.RawConn, error)
		}); ok {
			rawConn, err := sc.SyscallConn()
			if err != nil {
				return nil
			}
			return readTCPStats(rawConn)
		}
		if u, ok := conn.(interface{ Upstream() any }); ok {
			if next, ok2 := u.Upstream().(net.Conn); ok2 {
				conn = next
				continue outer
			}
		}
		if nc, ok := conn.(interface{ NetConn() net.Conn }); ok {
			conn = nc.NetConn()
			continue outer
		}
		v := reflect.ValueOf(conn)
		if v.Kind() == reflect.Ptr {
			v = v.Elem()
		}
		if v.Kind() == reflect.Struct {
			t := v.Type()
			for i := 0; i < v.NumField(); i++ {
				field := v.Field(i)
				if !t.Field(i).IsExported() || !field.CanInterface() {
					continue
				}
				if inner, ok := field.Interface().(net.Conn); ok {
					conn = inner
					continue outer
				}
			}
		}
		return nil
	}
	return nil
}
