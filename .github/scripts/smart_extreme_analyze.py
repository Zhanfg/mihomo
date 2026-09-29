import json
import sys

def main():
    phase = int(sys.argv[1])
    data = json.load(open(sys.argv[2]))
    source_counts = data.get("phase_used_counters", data["phase_counters"])
    source_sequence = data.get("phase_used_sequence", data["phase_sequence"])
    counts = {int(k): int(v) for k, v in source_counts[str(phase)].items()}
    seq = [int(v) for v in source_sequence[str(phase)]]
    best = {0: 18101, 1: 18102, 2: 18101}.get(phase)
    total = sum(counts.values())
    if total <= 0:
        raise SystemExit(f"phase={phase} has no proxy observations")
    if best is None:
        print(f"reaction phase={phase} total={total} counts={counts} neutral=1")
        return

    best_share = counts.get(best, 0) / total
    window = 128
    reaction = None
    for end in range(window, len(seq) + 1):
        sample = seq[end-window:end]
        if sum(1 for p in sample if p == best) / window >= 0.50:
            reaction = end - window
            break

    wrong = sum(v for p, v in counts.items() if p != best)
    print(
        f"reaction phase={phase} best={best} total={total} "
        f"best_share={best_share:.4f} first_stable_window={reaction} "
        f"wrong={wrong} counts={counts}"
    )
    if best_share < 0.40:
        raise SystemExit(f"phase={phase} best share too low: {best_share:.4f}")
    if reaction is None or reaction > 3000:
        raise SystemExit(f"phase={phase} reaction too slow: {reaction}")

if __name__ == "__main__":
    main()
