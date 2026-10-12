#!/usr/bin/env python3
"""
AnyTLS Padding Scheme Generator

Generates cryptographically random, realistic padding schemes for AnyTLS (sing-box / FNode),
designed to defeat DPI traffic classification and TLS-in-TLS fingerprinting.

Follows the protocol specification from github.com/anytls/sing-anytls:
- 'stop=N': Specifies the packet count threshold (indices 0 to N-1).
- '0=min-max': Auth frame padding (password SHA-256 + 2B length + padding0).
- '1=min-max': Initial session setup (cmdSettings + cmdSYN + SocksAddr).
- '2=min-max,c,...': Application data (TLS ClientHello) split with ',c,' (CheckMark early exit).
- '3..N-1': Early response, stabilization, and MTU-escalating tail.
"""

import argparse
import hashlib
import json
import random
import sys
from typing import List, Tuple, Dict, Any


def safe_range(lo_min: int, lo_max: int, hi_min: int, hi_max: int) -> str:
    """Safely generates a random 'min-max' range string without ValueError."""
    lo = random.randint(lo_min, max(lo_min, lo_max))
    hi = random.randint(max(lo + 1, hi_min), max(lo + 2, hi_max))
    return f"{lo}-{hi}"


def gen_escalating_tail(n: int, lo_start: int, lo_end: int, hi_start: int, hi_end: int, variance: int = 25) -> List[Tuple[int, int]]:
    """
    Generate n (lo, hi) pairs interpolating linearly from start -> end.
    Mimics TCP congestion window expansion during bulk data transfer.
    """
    result = []
    for i in range(n):
        t = i / max(n - 1, 1)
        base_lo = int(lo_start + t * (lo_end - lo_start))
        base_hi = int(hi_start + t * (hi_end - hi_start))
        lo = max(1, random.randint(max(1, base_lo - variance), base_lo + variance))
        hi_floor = max(lo + 20, base_hi - variance)
        hi_ceil = max(hi_floor + 10, base_hi + variance)
        hi = random.randint(hi_floor, hi_ceil)
        result.append((lo, hi))
    return result


def generate_universal(stop: int = 9) -> List[str]:
    """
    Universal padding scheme suitable for all general traffic
    (web browsing, video streaming, API calls, mixed traffic).
    """
    if stop < 7:
        raise ValueError("stop must be >= 7 to cover the TLS handshake and initial session window")

    lines = [f"stop={stop}"]

    # pkt 0: Auth frame (34B base: 32B sha256 + 2B length field).
    # Single segment, no fragmentation. Adds 28-72B padding.
    lines.append(f"0={safe_range(28, 44, 52, 72)}")

    # pkt 1: Session setup (cmdSettings + cmdSYN + SocksAddr).
    # Single range covering short IP:port and domain addresses.
    lines.append(f"1={safe_range(80, 140, 180, 360)}")

    # pkt 2: First application payload (e.g. TLS ClientHello).
    # 3 segments separated by 'c' (CheckMark) gates.
    # If payload is consumed early, 'c' terminates padding without dummy waste packets.
    seg_a = safe_range(180, 300, 320, 480)
    seg_b = safe_range(260, 400, 420, 620)
    seg_c = safe_range(180, 300, 340, 520)
    lines.append(f"2={seg_a},c,{seg_b},c,{seg_c}")

    # pkt 3: Early response / ACK / game tick (wide range: tiny to medium).
    lo3 = random.randint(28, 60)
    hi3 = random.randint(380, 680)
    lines.append(f"3={lo3}-{hi3}")

    # pkt 4: Second burst (audio frame, image chunk, API reply).
    lo4 = random.randint(60, 160)
    hi4 = random.randint(460, 820)
    lines.append(f"4={lo4}-{hi4}")

    # pkt 5: Mid-session stabilization.
    lo5 = random.randint(100, 220)
    hi5 = random.randint(520, 900)
    lines.append(f"5={lo5}-{hi5}")

    # pkt 6 -> stop-1: Escalating bulk transfer tail towards MTU.
    if stop > 6:
        tail = gen_escalating_tail(
            n=stop - 6,
            lo_start=180, lo_end=480,
            hi_start=580, hi_end=1260,
            variance=28
        )
        for i, pkt in enumerate(range(6, stop)):
            lo, hi = tail[i]
            lines.append(f"{pkt}={lo}-{hi}")

    return lines


def generate_low_latency(stop: int = 7) -> List[str]:
    """
    Optimized for gaming, VoIP, and interactive SSH where latency and minimal overhead matter.
    """
    if stop < 7:
        stop = 7

    lines = [f"stop={stop}"]
    lines.append(f"0={safe_range(20, 35, 40, 55)}")
    lines.append(f"1={safe_range(70, 120, 150, 260)}")

    # Pkt 2 with 2 segments and 1 checkmark
    seg_a = safe_range(160, 260, 280, 400)
    seg_b = safe_range(200, 320, 340, 500)
    lines.append(f"2={seg_a},c,{seg_b}")

    lines.append(f"3={safe_range(30, 60, 200, 450)}")
    lines.append(f"4={safe_range(50, 120, 300, 600)}")
    lines.append(f"5={safe_range(80, 160, 400, 750)}")

    if stop > 6:
        tail = gen_escalating_tail(stop - 6, 120, 300, 450, 950, variance=20)
        for i, pkt in enumerate(range(6, stop)):
            lo, hi = tail[i]
            lines.append(f"{pkt}={lo}-{hi}")

    return lines


def generate_stealth(stop: int = 11) -> List[str]:
    """
    Deep padding window for hostile network environments with strict DPI / active probing.
    """
    if stop < 10:
        stop = 11

    lines = [f"stop={stop}"]
    lines.append(f"0={safe_range(32, 50, 55, 80)}")
    lines.append(f"1={safe_range(100, 160, 220, 420)}")

    # 4-segment split for ClientHello with multiple c-gates
    seg_a = safe_range(180, 280, 300, 420)
    seg_b = safe_range(240, 360, 380, 540)
    seg_c = safe_range(200, 320, 340, 500)
    seg_d = safe_range(180, 280, 300, 450)
    lines.append(f"2={seg_a},c,{seg_b},c,{seg_c},c,{seg_d}")

    lines.append(f"3={safe_range(40, 80, 400, 750)}")
    lines.append(f"4={safe_range(80, 180, 500, 850)}")
    lines.append(f"5={safe_range(120, 240, 600, 950)}")

    tail = gen_escalating_tail(stop - 6, 200, 550, 650, 1380, variance=30)
    for i, pkt in enumerate(range(6, stop)):
        lo, hi = tail[i]
        lines.append(f"{pkt}={lo}-{hi}")

    return lines


def compute_md5(lines: List[str]) -> str:
    """Matches AnyTLS internal md5 checksum: md5.Sum(rawScheme)."""
    return hashlib.md5("\n".join(lines).encode("utf-8")).hexdigest()


def validate_scheme(lines: List[str]) -> Tuple[bool, str]:
    """
    Validates the padding scheme against AnyTLS Go protocol implementation rules:
    - Must contain valid 'stop=N' with N >= 2.
    - Must contain sequential keys from 0 to stop-1.
    - Each rule must consist of valid ranges 'min-max' or 'c'.
    - Numbers must be positive integers.
    """
    mapping: Dict[str, str] = {}
    for line in lines:
        parts = line.split("=", 1)
        if len(parts) != 2:
            return False, f"Invalid line format (missing '='): '{line}'"
        k, v = parts[0].strip(), parts[1].strip()
        mapping[k] = v

    if "stop" not in mapping:
        return False, "Missing 'stop=' definition"

    try:
        stop_val = int(mapping["stop"])
    except ValueError:
        return False, f"Invalid 'stop' value: '{mapping['stop']}'"

    if stop_val < 2:
        return False, f"'stop' must be at least 2, got {stop_val}"

    for i in range(stop_val):
        key = str(i)
        if key not in mapping:
            return False, f"Missing definition for packet index {key} (expected 0..{stop_val-1})"

        val = mapping[key]
        segments = val.split(",")
        if not segments:
            return False, f"Empty rule for packet {key}"

        for seg in segments:
            seg = seg.strip()
            if seg == "c":
                continue
            range_parts = seg.split("-")
            if len(range_parts) != 2:
                return False, f"Packet {key}: invalid segment '{seg}' (must be 'min-max' or 'c')"
            try:
                _min = int(range_parts[0])
                _max = int(range_parts[1])
            except ValueError:
                return False, f"Packet {key}: non-integer in range '{seg}'"

            if _min <= 0 or _max <= 0:
                return False, f"Packet {key}: range bounds must be positive, got {_min}-{_max}"

    return True, "Valid AnyTLS padding scheme"


def format_singbox_json(lines: List[str]) -> str:
    """Formats the scheme into a sing-box inbound JSON snippet."""
    data = {"padding_scheme": lines}
    return json.dumps(data, indent=2)


def main():
    parser = argparse.ArgumentParser(
        description="AnyTLS Padding Scheme Generator for sing-box and FNode",
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument(
        "--preset",
        choices=["universal", "low-latency", "stealth"],
        default="universal",
        help="Traffic profile preset (default: universal)",
    )
    parser.add_argument(
        "--stop",
        type=int,
        default=None,
        help="Number of initial packets to apply padding to (e.g. 8, 9, 10)",
    )
    parser.add_argument(
        "--format",
        choices=["json", "raw", "full"],
        default="full",
        help="Output format: json (sing-box array), raw (plain lines), full (detailed view)",
    )
    parser.add_argument(
        "--test",
        action="store_true",
        help="Run self-tests and validation",
    )

    args = parser.parse_args()

    if args.test:
        print("[*] Running AnyTLS generator self-tests...")
        for preset_name, gen_func, default_stop in [
            ("universal", generate_universal, 9),
            ("low-latency", generate_low_latency, 7),
            ("stealth", generate_stealth, 11),
        ]:
            for s in [default_stop, default_stop + 1]:
                scheme = gen_func(stop=s)
                valid, msg = validate_scheme(scheme)
                if not valid:
                    print(f"[-] FAIL: {preset_name} (stop={s}): {msg}", file=sys.stderr)
                    sys.exit(1)
                md5_sum = compute_md5(scheme)
                print(f"[+] PASS: {preset_name:<12} (stop={s}) -> MD5: {md5_sum}")
        print("[+] All self-tests passed successfully!")
        return

    # Select generator
    if args.preset == "low-latency":
        stop = args.stop or 7
        scheme = generate_low_latency(stop=stop)
    elif args.preset == "stealth":
        stop = args.stop or 11
        scheme = generate_stealth(stop=stop)
    else:
        stop = args.stop or 9
        scheme = generate_universal(stop=stop)

    valid, msg = validate_scheme(scheme)
    if not valid:
        print(f"Error: Generated scheme is invalid: {msg}", file=sys.stderr)
        sys.exit(1)

    if args.format == "raw":
        print("\n".join(scheme))
    elif args.format == "json":
        print(format_singbox_json(scheme))
    else:
        md5_val = compute_md5(scheme)
        print("=" * 60)
        print(f"  AnyTLS Padding Scheme ({args.preset.upper()} | stop={scheme[0].split('=')[1]})")
        print("=" * 60)
        for line in scheme:
            print(f"  {line}")
        print("-" * 60)
        print(f"  MD5 Checksum: {md5_val}")
        print("=" * 60)
        print("\n--- sing-box inbound configuration snippet ---")
        print(format_singbox_json(scheme))


if __name__ == "__main__":
    main()
