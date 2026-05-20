#!/usr/bin/env python3
"""
Read a power meter CSV (*-timestamp.csv: timestamp as Unix seconds + meter-platform_power),
derive the time window, fetch estimator_procstat wide CSV from Prometheus
(via scripts/fetch_prometheus_metrics.py), then merge into a training CSV.

Default output columns match scripts/train_power_linear_regression.py:
  timestamp, sys_time, usr_time, power_watt

(Prometheus columns are detected as estimator_procstat_*system* / *user*, e.g.
estimator_procstat_system_jiffies_delta and estimator_procstat_user_jiffies_delta.)

Uses only the standard library.

Examples:
  # Fetch from Prometheus and write training CSV
  python3 scripts/merge_power_csv_with_procstat_prometheus.py \\
    run_123-timestamp.csv -o training.csv --preview 8

  # Reuse an already-fetched metrics CSV instead of calling Prometheus
  python3 scripts/merge_power_csv_with_procstat_prometheus.py \\
    run_123-timestamp.csv --prom-csv metrics.csv -o training.csv

  # Nearest-sample join when timestamps do not align exactly (seconds)
  python3 scripts/merge_power_csv_with_procstat_prometheus.py \\
    run_123-timestamp.csv --merge-tolerance-sec 8 -o training.csv
"""

from __future__ import annotations

import argparse
import bisect
import csv
import io
import re
import subprocess
import sys
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import TextIO


SCRIPTS_DIR = Path(__file__).resolve().parent
FETCH_SCRIPT = SCRIPTS_DIR / "fetch_prometheus_metrics.py"

POWER_TS_NAMES = ("timestamp", "ts")
POWER_VALUE_NAMES = ("meter-platform_power",)
SYS_RE = re.compile(r"estimator_procstat.*system", re.IGNORECASE)
USR_RE = re.compile(r"estimator_procstat.*user", re.IGNORECASE)
_UNIX_TS_RE = re.compile(r"^[+-]?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?$")


def parse_timestamp(value: str, fmt: str = "auto") -> datetime:
    """Parse timestamp; fmt is unix, rfc3339, or auto (numeric → unix, else ISO-8601)."""
    v = value.strip()
    if fmt == "auto":
        fmt = "unix" if _UNIX_TS_RE.match(v) else "rfc3339"
    if fmt == "unix":
        return datetime.fromtimestamp(float(v), tz=timezone.utc)
    if v.endswith("Z"):
        v = v[:-1] + "+00:00"
    dt = datetime.fromisoformat(v)
    if dt.tzinfo is None:
        dt = dt.replace(tzinfo=timezone.utc)
    return dt.astimezone(timezone.utc)


def pick_header_col(header: list[str], candidates: tuple[str, ...], contains: str | None = None) -> str:
    lower = [h.strip().lower() for h in header]
    for name in candidates:
        if name.lower() in lower:
            idx = lower.index(name.lower())
            return header[idx]
    if contains:
        for h in header:
            if contains.lower() in h.lower():
                return h
    raise SystemExit(
        f"Could not find column from {candidates} (contains={contains!r}) in header: {header}"
    )


def pick_sys_usr_columns(header: list[str]) -> tuple[str, str]:
    skip = {"timestamp", "time"}
    sys_col: str | None = None
    usr_col: str | None = None
    for h in header:
        hl = h.strip().lower()
        if hl in skip:
            continue
        if SYS_RE.search(h) and sys_col is None:
            sys_col = h
        if USR_RE.search(h) and usr_col is None:
            usr_col = h
    if sys_col is None or usr_col is None:
        raise SystemExit(
            "Could not auto-detect estimator_procstat system/user columns. "
            f"Header was: {header}. Use --sys-col / --usr-col."
        )
    return sys_col, usr_col


@dataclass(frozen=True)
class PowerRow:
    t: float
    ts_text: str
    power: float


def read_power_csv(path: Path, power_col: str | None, *, ts_format: str) -> list[PowerRow]:
    with path.open(newline="", encoding="utf-8") as f:
        r = csv.DictReader(f)
        if r.fieldnames is None:
            raise SystemExit("Power CSV has no header")
        header = list(r.fieldnames)
        ts_col = pick_header_col(header, POWER_TS_NAMES)
        pcol = power_col or pick_header_col(header, POWER_VALUE_NAMES, contains="meter-platform_power")
        rows: list[PowerRow] = []
        for i, row in enumerate(r, start=2):
            try:
                ts_raw = row[ts_col].strip()
                pow_raw = row[pcol].strip()
                dt = parse_timestamp(ts_raw, ts_format)
                pw = float(pow_raw)
            except (KeyError, ValueError, AttributeError) as e:
                raise SystemExit(f"{path}:{i}: bad timestamp or power ({e})") from e
            rows.append(PowerRow(t=dt.timestamp(), ts_text=ts_raw, power=pw))
    rows.sort(key=lambda x: x.t)
    return rows


@dataclass(frozen=True)
class PromRow:
    t: float
    ts_text: str
    sys_time: float
    usr_time: float


def read_prom_wide_csv(
    source: Path | TextIO,
    sys_col: str | None,
    usr_col: str | None,
    *,
    ts_format: str = "auto",
    source_label: str = "",
) -> list[PromRow]:
    close_f = isinstance(source, Path)
    if close_f:
        f = source.open(newline="", encoding="utf-8")
        label = str(source)
    else:
        f = source
        label = source_label or "<stream>"
    try:
        r = csv.DictReader(f)
        if r.fieldnames is None:
            raise SystemExit(f"{label}: Prometheus CSV has no header")
        header = list(r.fieldnames)
        ts_col = pick_header_col(header, POWER_TS_NAMES)
        sc, uc = (
            (sys_col, usr_col)
            if sys_col and usr_col
            else pick_sys_usr_columns(header)
        )
        out: list[PromRow] = []
        for i, row in enumerate(r, start=2):
            try:
                ts_raw = row[ts_col].strip()
                dt = parse_timestamp(ts_raw, ts_format)
                st = float(row[sc].strip())
                ut = float(row[uc].strip())
            except (KeyError, ValueError, AttributeError) as e:
                raise SystemExit(f"{label}:{i}: bad row ({e})") from e
            out.append(PromRow(t=dt.timestamp(), ts_text=ts_raw, sys_time=st, usr_time=ut))
    finally:
        if close_f:
            f.close()
    out.sort(key=lambda x: x.t)
    return out


def fetch_prometheus_csv(
    *,
    url: str,
    start: datetime,
    end: datetime,
    step: str,
    name_prefixes: list[str],
    csv_timestamp: str,
    print_cmd: bool,
) -> str:
    start_u = int(start.timestamp())
    end_u = int(end.timestamp())
    cmd = [
        sys.executable,
        str(FETCH_SCRIPT),
        "--url",
        url,
        "--start",
        str(start_u),
        "--end",
        str(end_u),
        "--step",
        step,
        "--format",
        "csv",
        "--csv-timestamp",
        csv_timestamp,
    ]
    for pfx in name_prefixes:
        cmd.extend(["--name-prefix", pfx])
    if print_cmd:
        print("Running:", " ".join(cmd), file=sys.stderr)
    proc = subprocess.run(
        cmd,
        check=False,
        capture_output=True,
        text=True,
    )
    if proc.returncode != 0:
        raise SystemExit(
            f"fetch_prometheus_metrics.py failed (exit {proc.returncode})\n"
            f"stderr:\n{proc.stderr}\nstdout:\n{proc.stdout[:4000]}"
        )
    return proc.stdout


def merge_exact(power: list[PowerRow], prom: list[PromRow]) -> list[tuple[str, float, float, float]]:
    by_sec: dict[int, PromRow] = {}
    for pr in prom:
        by_sec[int(pr.t)] = pr
    merged: list[tuple[str, float, float, float]] = []
    for pw in power:
        pr = by_sec.get(int(pw.t))
        if pr is None:
            continue
        merged.append((pw.ts_text, pr.sys_time, pr.usr_time, pw.power))
    return merged


def merge_nearest(power: list[PowerRow], prom: list[PromRow], tol_sec: float) -> list[tuple[str, float, float, float]]:
    if not prom:
        return []
    ts_list = [p.t for p in prom]
    merged: list[tuple[str, float, float, float]] = []
    for pw in power:
        i = bisect.bisect_left(ts_list, pw.t)
        best_j: int | None = None
        best_d = tol_sec + 1.0
        for j in (i - 1, i):
            if 0 <= j < len(prom):
                d = abs(ts_list[j] - pw.t)
                if d < best_d:
                    best_d = d
                    best_j = j
        if best_j is None or best_d > tol_sec:
            continue
        pr = prom[best_j]
        merged.append((pw.ts_text, pr.sys_time, pr.usr_time, pw.power))
    return merged


def preview_lines(text: str, n: int, *, stream: TextIO) -> None:
    ls = text.splitlines()
    head = ls[: n + 1]  # header + n rows
    print("\n".join(head), file=stream)
    if len(ls) > len(head):
        print(f"... ({len(ls) - len(head)} more lines)", file=stream)


def main() -> None:
    p = argparse.ArgumentParser(
        description="Merge *-timestamp power CSV with Prometheus estimator_procstat CSV for linear regression."
    )
    p.add_argument(
        "power_csv",
        type=Path,
        help="Power meter CSV (e.g. xxx-timestamp.csv) with timestamp and meter-platform_power",
    )
    p.add_argument(
        "-o",
        "--output",
        type=Path,
        default=None,
        help="Write merged CSV to this file (if omitted, full CSV is written to stdout)",
    )
    p.add_argument(
        "--preview",
        type=int,
        metavar="N",
        default=0,
        help="Print first N data rows (+ header) to stderr after merge",
    )
    p.add_argument(
        "--training-columns",
        action=argparse.BooleanOptionalAction,
        default=True,
        help="Use sys_time/usr_time/power_watt for train_power_linear_regression.py (default: true)",
    )
    p.add_argument("--power-col", default=None, help="Override power column name (default: meter-platform_power)")
    p.add_argument(
        "--power-ts-format",
        choices=("unix", "rfc3339", "auto"),
        default="unix",
        help="Timestamp format in power CSV (default: unix seconds for *-timestamp.csv)",
    )

    p.add_argument(
        "--prom-csv",
        type=Path,
        default=None,
        help="Use this wide Prometheus CSV instead of fetching",
    )
    p.add_argument("--url", default="http://10.43.23.61:9090", help="Prometheus base URL")
    p.add_argument(
        "--name-prefix",
        action="append",
        default=None,
        help="Metric name prefix for fetch (repeatable). Default: estimator_procstat",
    )
    p.add_argument("--step", default="15s", help="Prometheus range step (default: 15s)")
    p.add_argument(
        "--csv-timestamp",
        choices=("rfc3339", "unix"),
        default="rfc3339",
        help="Timestamp format for fetched CSV (default: rfc3339)",
    )
    p.add_argument(
        "--merge-tolerance-sec",
        type=float,
        default=0.0,
        help="If >0, match each power row to nearest Prometheus row within this many seconds",
    )
    p.add_argument("--sys-col", default=None, help="Prometheus CSV column for system feature")
    p.add_argument("--usr-col", default=None, help="Prometheus CSV column for user feature")
    p.add_argument(
        "--print-fetch-cmd",
        action="store_true",
        help="Print the fetch_prometheus_metrics.py invocation to stderr",
    )
    args = p.parse_args()

    power_rows = read_power_csv(args.power_csv, args.power_col, ts_format=args.power_ts_format)
    if not power_rows:
        raise SystemExit("No rows in power CSV")

    start_dt = datetime.fromtimestamp(power_rows[0].t, tz=timezone.utc)
    end_dt = datetime.fromtimestamp(power_rows[-1].t, tz=timezone.utc)

    prefixes = args.name_prefix if args.name_prefix else ["estimator_procstat"]

    if args.prom_csv:
        prom_rows = read_prom_wide_csv(
            args.prom_csv,
            args.sys_col,
            args.usr_col,
            ts_format="auto",
        )
    else:
        raw = fetch_prometheus_csv(
            url=args.url,
            start=start_dt,
            end=end_dt,
            step=args.step,
            name_prefixes=prefixes,
            csv_timestamp=args.csv_timestamp,
            print_cmd=args.print_fetch_cmd,
        )
        prom_rows = read_prom_wide_csv(
            io.StringIO(raw),
            args.sys_col,
            args.usr_col,
            ts_format=args.csv_timestamp,
            source_label="<fetch_prometheus_metrics.py stdout>",
        )

    if args.merge_tolerance_sec and args.merge_tolerance_sec > 0:
        merged = merge_nearest(power_rows, prom_rows, args.merge_tolerance_sec)
    else:
        merged = merge_exact(power_rows, prom_rows)

    if len(merged) < 3:
        raise SystemExit(
            f"Too few merged rows ({len(merged)}); need at least 3 for training. "
            "Try --merge-tolerance-sec 8, check time overlap, or verify Prometheus step/labels."
        )

    buf = io.StringIO()
    fieldnames = (
        ("timestamp", "sys_time", "usr_time", "power_watt")
        if args.training_columns
        else ("timestamp", "system", "user", "power")
    )
    w = csv.writer(buf, lineterminator="\n")
    w.writerow(fieldnames)
    for ts, st, ut, pw in merged:
        w.writerow([ts, st, ut, pw])
    text = buf.getvalue()

    if args.preview > 0:
        preview_lines(text, args.preview, stream=sys.stderr)

    if args.output:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(text, encoding="utf-8")
        print(f"Wrote {len(merged)} rows to {args.output}", file=sys.stderr)
    else:
        sys.stdout.write(text)


if __name__ == "__main__":
    main()
