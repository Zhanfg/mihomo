#!/usr/bin/env python3
import argparse
import concurrent.futures
import json
import socket
import statistics
import time


def one_request(proxy_host: str, proxy_port: int, target: str, timeout: float):
    start = time.perf_counter()
    ok = False
    err = ""
    try:
        with socket.create_connection((proxy_host, proxy_port), timeout=timeout) as s:
            s.settimeout(timeout)
            req = (
                f"GET {target} HTTP/1.1\r\n"
                "Host: 127.0.0.1:18080\r\n"
                "Connection: close\r\n"
                "User-Agent: smart-extreme-soak\r\n\r\n"
            ).encode()
            s.sendall(req)
            data = b""
            while b"\r\n" not in data and len(data) < 4096:
                chunk = s.recv(4096)
                if not chunk:
                    break
                data += chunk
            line = data.split(b"\r\n", 1)[0]
            ok = b" 200 " in line
            if not ok:
                err = line.decode("latin1", "replace")
    except Exception as exc:
        err = type(exc).__name__ + ":" + str(exc)
    elapsed_ms = (time.perf_counter() - start) * 1000.0
    return ok, elapsed_ms, err


def percentile(values, q):
    if not values:
        return 0.0
    xs = sorted(values)
    pos = int(round((len(xs) - 1) * q))
    return xs[max(0, min(len(xs) - 1, pos))]


def main():
    p = argparse.ArgumentParser()
    p.add_argument("--count", type=int, required=True)
    p.add_argument("--concurrency", type=int, required=True)
    p.add_argument("--timeout", type=float, default=4.0)
    p.add_argument("--proxy-host", default="127.0.0.1")
    p.add_argument("--proxy-port", type=int, default=17890)
    p.add_argument("--target", default="http://127.0.0.1:18080/")
    p.add_argument("--label", default="wave")
    args = p.parse_args()

    started = time.perf_counter()
    latencies = []
    failures = 0
    errors = {}

    with concurrent.futures.ThreadPoolExecutor(max_workers=args.concurrency) as ex:
        futures = [
            ex.submit(one_request, args.proxy_host, args.proxy_port, args.target, args.timeout)
            for _ in range(args.count)
        ]
        for fut in concurrent.futures.as_completed(futures):
            ok, latency, err = fut.result()
            latencies.append(latency)
            if not ok:
                failures += 1
                errors[err] = errors.get(err, 0) + 1

    elapsed = time.perf_counter() - started
    result = {
        "label": args.label,
        "requests": args.count,
        "concurrency": args.concurrency,
        "failures": failures,
        "failure_rate": failures / max(1, args.count),
        "elapsed_s": elapsed,
        "rps": args.count / max(elapsed, 1e-9),
        "latency_ms": {
            "p50": percentile(latencies, 0.50),
            "p95": percentile(latencies, 0.95),
            "p99": percentile(latencies, 0.99),
            "max": max(latencies) if latencies else 0.0,
            "mean": statistics.fmean(latencies) if latencies else 0.0,
        },
        "top_errors": sorted(errors.items(), key=lambda kv: kv[1], reverse=True)[:8],
    }
    print(json.dumps(result, separators=(",", ":"), sort_keys=True))


if __name__ == "__main__":
    main()
