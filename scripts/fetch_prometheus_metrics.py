#!/usr/bin/env python3
"""
Fetch Prometheus series with timestamps via the HTTP API.

Uses only the standard library (no pip packages).

Examples:
  # Instant query (single timestamp per series)
  python3 scripts/fetch_prometheus_metrics.py \\
    --url http://localhost:9090 --query 'up'

  # Multiple instant queries (repeat -q or use --queries-file)
  python3 scripts/fetch_prometheus_metrics.py \\
    --url http://localhost:9090 -q 'up' -q 'process_resident_memory_bytes'

  # All metrics whose __name__ starts with the same prefix (one PromQL regex query)
  python3 scripts/fetch_prometheus_metrics.py \\
    --url http://localhost:9090 --name-prefix kepler_

  # Range query (many [unix_ts, value] pairs per series)
  python3 scripts/fetch_prometheus_metrics.py \\
    --url http://localhost:9090 --query 'rate(http_requests_total[5m])' \\
    --start $(date -d '10 minutes ago' +%s) --end $(date +%s) --step 15s

  # Wide table: first column timestamp, one column per time series (CSV)
  python3 scripts/fetch_prometheus_metrics.py \\
    --url http://localhost:9090 --name-prefix kepler_ \\
    --start ... --end ... --step 15s --format csv
"""

from __future__ import annotations

import argparse
import csv
import json
import re
import sys
from collections import defaultdict
from datetime import datetime, timezone
import urllib.error
import urllib.parse
import urllib.request
from typing import Any, TextIO


def _get_json(url: str) -> dict[str, Any]:
    req = urllib.request.Request(url, headers={"Accept": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=60) as resp:
            body = resp.read().decode("utf-8")
    except urllib.error.HTTPError as e:
        detail = e.read().decode("utf-8", errors="replace")
        raise SystemExit(f"HTTP {e.code} {e.reason}: {detail}") from e
    except urllib.error.URLError as e:
        raise SystemExit(f"Request failed: {e}") from e
    return json.loads(body)


def instant_query(base: str, query: str, time_s: str | None) -> dict[str, Any]:
    q = urllib.parse.urlencode({"query": query, **({"time": time_s} if time_s else {})})
    return _get_json(f"{base.rstrip('/')}/api/v1/query?{q}")


def range_query(base: str, query: str, start: str, end: str, step: str) -> dict[str, Any]:
    q = urllib.parse.urlencode({"query": query, "start": start, "end": end, "step": step})
    return _get_json(f"{base.rstrip('/')}/api/v1/query_range?{q}")


def flatten_instant(payload: dict[str, Any]) -> list[dict[str, Any]]:
    out: list[dict[str, Any]] = []
    if payload.get("status") != "success":
        return out
    for r in payload.get("data", {}).get("result", []):
        metric = r.get("metric", {})
        val = r.get("value")
        if not val or len(val) < 2:
            continue
        ts, v = val[0], val[1]
        out.append({"timestamp_unix": float(ts), "value": v, "metric": metric})
    return out


def flatten_range(payload: dict[str, Any]) -> list[dict[str, Any]]:
    out: list[dict[str, Any]] = []
    if payload.get("status") != "success":
        return out
    for r in payload.get("data", {}).get("result", []):
        metric = r.get("metric", {})
        for pair in r.get("values", []):
            if not pair or len(pair) < 2:
                continue
            ts, v = pair[0], pair[1]
            out.append({"timestamp_unix": float(ts), "value": v, "metric": metric})
    return out


def load_queries_from_file(path: str) -> list[str]:
    out: list[str] = []
    with open(path, encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if not line or line.startswith("#"):
                continue
            out.append(line)
    return out


def promql_for_metric_name_prefix(prefix: str) -> str:
    """Match any series whose metric name (__name__) starts with this literal prefix."""
    escaped = re.escape(prefix)
    return f'{{__name__=~"^{escaped}.*"}}'


def collect_queries(args: argparse.Namespace) -> list[str]:
    queries: list[str] = []
    if args.query:
        queries.extend(args.query)
    if args.queries_file:
        queries.extend(load_queries_from_file(args.queries_file))
    if args.name_prefix:
        for raw in args.name_prefix:
            pfx = raw.strip()
            if not pfx:
                continue
            queries.append(promql_for_metric_name_prefix(pfx))
    return queries


def _escape_label_value(s: str) -> str:
    return s.replace("\\", "\\\\").replace('"', '\\"')


def series_column_name(labels: dict[str, Any]) -> str:
    """Human-readable column id: metric or metric{label=\"value\",...}."""
    name = str(labels.get("__name__", ""))
    rest = [(str(k), str(v)) for k, v in sorted(labels.items()) if k != "__name__"]
    if not rest:
        return name or json.dumps(labels, sort_keys=True, ensure_ascii=False)
    inner = ",".join(f'{k}="{_escape_label_value(v)}"' for k, v in rest)
    return f"{name}{{{inner}}}"


def format_timestamp_cell(ts: float, mode: str) -> str:
    if mode == "unix":
        if ts == int(ts):
            return str(int(ts))
        return str(ts)
    dt = datetime.fromtimestamp(ts, tz=timezone.utc)
    return dt.isoformat().replace("+00:00", "Z")


def write_wide_csv(
    records: list[tuple[float, str, str]],
    timestamp_mode: str,
    delimiter: str,
    out: TextIO,
) -> None:
    """Pivot to wide table: column 0 = timestamp, then one column per series name."""
    matrix: dict[float, dict[str, str]] = defaultdict(dict)
    col_order: list[str] = []
    seen: set[str] = set()
    for ts, col, val in records:
        matrix[ts][col] = val
        if col not in seen:
            seen.add(col)
            col_order.append(col)
    col_order.sort()
    w = csv.writer(out, delimiter=delimiter)
    w.writerow(["timestamp", *col_order])
    for ts in sorted(matrix.keys()):
        rowm = matrix[ts]
        w.writerow([format_timestamp_cell(ts, timestamp_mode)] + [rowm.get(c, "") for c in col_order])


def main() -> None:
    p = argparse.ArgumentParser(description="Fetch Prometheus metrics with timestamps.")
    p.add_argument("--url", default="http://localhost:9090", help="Prometheus base URL")
    p.add_argument(
        "-q",
        "--query",
        action="append",
        default=None,
        metavar="PROMQL",
        help="PromQL expression (repeat for multiple metrics)",
    )
    p.add_argument(
        "--queries-file",
        metavar="PATH",
        help="File with one PromQL per line; empty lines and # comments ignored",
    )
    p.add_argument(
        "--name-prefix",
        action="append",
        default=None,
        metavar="PREFIX",
        help=(
            "Literal metric-name prefix: adds query {__name__=~\"^PREFIX.*\"} "
            "(repeat for multiple prefixes). Escapes regex metacharacters in PREFIX."
        ),
    )
    p.add_argument("--time", help="Evaluation time for instant query (Unix seconds or RFC3339)")
    p.add_argument("--start", help="Range start (Unix seconds or RFC3339)")
    p.add_argument("--end", help="Range end (Unix seconds or RFC3339)")
    p.add_argument("--step", default="15s", help="Range resolution (e.g. 15s, 1m)")
    p.add_argument(
        "--raw",
        action="store_true",
        help="Print full Prometheus JSON (one query: object; multiple: array of {query, response})",
    )
    p.add_argument(
        "--format",
        choices=("jsonl", "csv"),
        default="jsonl",
        help="jsonl: one JSON object per sample; csv: wide table (timestamp + one column per series)",
    )
    p.add_argument(
        "--csv-timestamp",
        choices=("rfc3339", "unix"),
        default="rfc3339",
        help="First column format when --format csv (default: rfc3339 UTC)",
    )
    p.add_argument(
        "--csv-delimiter",
        default=",",
        metavar="CHAR",
        help="Field separator for CSV (use $'\\t' for TSV)",
    )
    args = p.parse_args()
    queries = collect_queries(args)
    if not queries:
        p.error("provide at least one --query / -q, --queries-file, or --name-prefix")
    base = args.url

    is_range = args.start is not None and args.end is not None
    if args.start is not None and args.end is None:
        p.error("--start and --end must be given together for a range query")
    if args.end is not None and args.start is None:
        p.error("--start and --end must be given together for a range query")

    raw_results: list[dict[str, Any]] = []
    exit_code = 0
    csv_records: list[tuple[float, str, str]] = []
    use_csv = not args.raw and args.format == "csv"
    delim = args.csv_delimiter.encode("utf-8").decode("unicode_escape")

    for promql in queries:
        if is_range:
            payload = range_query(base, promql, args.start, args.end, args.step)
        else:
            payload = instant_query(base, promql, args.time)

        if args.raw:
            raw_results.append({"query": promql, "response": payload})
            continue

        if payload.get("status") != "success":
            err_obj = {"query": promql, "error": payload}
            if use_csv:
                print(json.dumps(err_obj, ensure_ascii=False), file=sys.stderr)
            else:
                json.dump(err_obj, sys.stdout, indent=2, ensure_ascii=False)
                sys.stdout.write("\n")
            exit_code = 1
            continue

        rows = flatten_range(payload) if is_range else flatten_instant(payload)
        if use_csv:
            for row in rows:
                col = series_column_name(row["metric"])
                csv_records.append((row["timestamp_unix"], col, str(row["value"])))
            continue

        for row in rows:
            line = {
                "promql": promql,
                "timestamp_unix": row["timestamp_unix"],
                "value": row["value"],
                "labels": row["metric"],
            }
            json.dump(line, sys.stdout, ensure_ascii=False)
            sys.stdout.write("\n")

    if use_csv:
        write_wide_csv(csv_records, args.csv_timestamp, delim, sys.stdout)

    if args.raw:
        if len(raw_results) == 1:
            json.dump(raw_results[0]["response"], sys.stdout, indent=2, ensure_ascii=False)
        else:
            json.dump(raw_results, sys.stdout, indent=2, ensure_ascii=False)
        sys.stdout.write("\n")
        for item in raw_results:
            if item["response"].get("status") != "success":
                exit_code = 1
                break

    raise SystemExit(exit_code)


if __name__ == "__main__":
    main()
