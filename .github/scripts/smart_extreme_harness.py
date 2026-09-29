import http.server
import json
import os
import select
import socket
import socketserver
import threading
import time

ROOT = "/tmp/smart-extreme"
PORTS = (18101, 18102, 18103)
counters = {port: 0 for port in PORTS}
phase_counters = {p: {port: 0 for port in PORTS} for p in range(4)}
phase_sequence = {p: [] for p in range(4)}
lock = threading.Lock()

def phase():
    try:
        return int(open(ROOT + "/phase").read().strip())
    except Exception:
        return 0

def behavior(port, p=None):
    if p is None:
        p = phase()
    table = {
        0: {18101: (0.002, 0), 18102: (0.030, 0), 18103: (0.080, 7)},
        1: {18101: (0.350, 4), 18102: (0.005, 0), 18103: (0.120, 6)},
        2: {18101: (0.010, 0), 18102: (0.250, 5), 18103: (0.060, 3)},
        3: {18101: (0.020, 0), 18102: (0.020, 0), 18103: (0.020, 0)},
    }
    return table.get(p, table[0])[port]

class Target(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    def do_GET(self):
        body = b"smart-extreme"
        self.send_response(200)
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Connection", "close")
        self.end_headers()
        self.wfile.write(body)
    def log_message(self, *_):
        pass

class TargetServer(http.server.ThreadingHTTPServer):
    daemon_threads = True
    request_queue_size = 1024

class Proxy(socketserver.BaseRequestHandler):
    def handle(self):
        port = self.server.server_address[1]
        current_phase = phase()
        with lock:
            counters[port] += 1
            phase_counters[current_phase][port] += 1
            if len(phase_sequence[current_phase]) < 4096:
                phase_sequence[current_phase].append(port)
            n = counters[port]
        delay, drop_every = behavior(port, current_phase)
        if delay:
            time.sleep(delay)
        if drop_every and n % drop_every == 0:
            return

        client = self.request
        client.settimeout(8)
        data = b""
        while b"\r\n\r\n" not in data and len(data) < 65536:
            chunk = client.recv(4096)
            if not chunk:
                return
            data += chunk
        first = data.split(b"\r\n", 1)[0].decode("latin1", "replace")
        parts = first.split()
        if len(parts) < 2 or parts[0].upper() != "CONNECT":
            client.sendall(b"HTTP/1.1 405 Method Not Allowed\r\nContent-Length:0\r\n\r\n")
            return
        hostport = parts[1]
        host, port_s = hostport.rsplit(":", 1)
        host = host.strip("[]")
        try:
            upstream = socket.create_connection((host, int(port_s)), timeout=5)
        except OSError:
            return
        client.sendall(b"HTTP/1.1 200 Connection Established\r\n\r\n")
        client.setblocking(False)
        upstream.setblocking(False)
        sockets = [client, upstream]
        try:
            while True:
                readable, _, exceptional = select.select(sockets, [], sockets, 5)
                if exceptional or not readable:
                    break
                for src in readable:
                    dst = upstream if src is client else client
                    try:
                        buf = src.recv(65536)
                    except BlockingIOError:
                        continue
                    if not buf:
                        return
                    dst.sendall(buf)
        finally:
            upstream.close()

class Threaded(socketserver.ThreadingMixIn, socketserver.TCPServer):
    daemon_threads = True
    allow_reuse_address = True
    request_queue_size = 1024

class Stats(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path != "/stats":
            self.send_response(404)
            self.end_headers()
            return
        with lock:
            payload = json.dumps(
                {
                    "counters": counters,
                    "phase_counters": phase_counters,
                    "phase_sequence": phase_sequence,
                },
                separators=(",", ":"),
            ).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)
    def log_message(self, *_):
        pass

def main():
    os.chdir(ROOT + "/www")
    target = TargetServer(("127.0.0.1", 18080), Target)
    stats = TargetServer(("127.0.0.1", 18082), Stats)
    threading.Thread(target=target.serve_forever, daemon=True).start()
    threading.Thread(target=stats.serve_forever, daemon=True).start()

    for port in PORTS:
        srv = Threaded(("127.0.0.1", port), Proxy)
        threading.Thread(target=srv.serve_forever, daemon=True).start()

    while True:
        time.sleep(60)

if __name__ == "__main__":
    main()
