package outbound

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"

	C "github.com/metacubex/mihomo/constant"
)

func TestHTTPConnectPreservesFirstPayload(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	requestRead := make(chan error, 1)
	go func() {
		reader := bufio.NewReader(server)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				requestRead <- err
				return
			}
			if line == "\r\n" {
				break
			}
		}
		_, err := io.WriteString(server, "HTTP/1.1 200 Connection established\r\n\r\nfirst-payload")
		requestRead <- err
	}()
	proxy := &Http{option: &HttpOption{}}
	metadata := &C.Metadata{Host: "example.org", DstPort: 443}
	conn, err := proxy.StreamConnContext(context.Background(), client, metadata)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len("first-payload"))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatal(err)
	}
	if got := string(buf); got != "first-payload" {
		t.Fatalf("CONNECT response swallowed early payload: %q", got)
	}
	if err := <-requestRead; err != nil {
		t.Fatal(err)
	}
}

type writeFailingConn struct {
	net.Conn
	closed bool
}

func (c *writeFailingConn) Write([]byte) (int, error) {
	return 0, io.ErrClosedPipe
}

func (c *writeFailingConn) Close() error {
	c.closed = true
	return nil
}

func TestSSHWriteErrorClosesUnderlyingConnection(t *testing.T) {
	underlying := &writeFailingConn{}
	conn := closeOnWriteErrorConn{Conn: underlying}
	_, err := conn.Write([]byte("test"))
	if !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("Write error=%v, want closed pipe", err)
	}
	if !underlying.closed {
		t.Fatal("underlying SSH connection remained open after write failure")
	}
}

func TestHTTPConnectReturnsProxyError(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	go func() {
		reader := bufio.NewReader(server)
		for {
			line, err := reader.ReadString('\n')
			if err != nil || line == "\r\n" {
				break
			}
		}
		_, _ = io.WriteString(server, "HTTP/1.1 407 Proxy Authentication Required\r\nContent-Length: 0\r\n\r\n")
	}()
	proxy := &Http{option: &HttpOption{}}
	metadata := &C.Metadata{Host: "example.org", DstPort: 443}
	_, err := proxy.StreamConnContext(context.Background(), client, metadata)
	if err == nil || !strings.Contains(err.Error(), "auth") {
		t.Fatalf("CONNECT auth failure was lost: %v", err)
	}
}
