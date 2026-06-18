#!/usr/bin/env python3
"""
Train ordinary least-squares linear regression: power_watt ~ sys_time + usr_time.

Reports k-fold cross-validation metrics (R², MAE, RMSE) and coefficients on the full dataset.

Dependencies:
  pip install numpy scikit-learn

Example:
  python3 scripts/train_power_linear_regression.py --csv data/train.csv --folds 5
  python3 scripts/train_power_linear_regression.py --csv data/train.csv --test-csv data/test.csv
"""

from __future__ import annotations

import argparse
import csv
import math
import sys
from pathlib import Path

import numpy as np
from sklearn.linear_model import LinearRegression
from sklearn.metrics import make_scorer, mean_absolute_error, mean_squared_error, r2_score
from sklearn.model_selection import KFold, cross_validate


FEATURES = ("sys_time", "usr_time")
DEFAULT_TARGET = "power_watt"


def _load_rows(
    path: Path, target_col: str, *, min_rows: int = 3, label: str = "CSV"
) -> tuple[np.ndarray, np.ndarray]:
    """Return X (n, 2) and y (n,) from CSV with columns sys_time, usr_time, and target."""
    xs: list[list[float]] = []
    ys: list[float] = []
    with path.open(newline="", encoding="utf-8") as f:
        reader = csv.DictReader(f)
        if reader.fieldnames is None:
            raise SystemExit(f"{label} has no header row")
        missing = [c for c in (*FEATURES, target_col) if c not in reader.fieldnames]
        if missing:
            raise SystemExit(
                f"{label} missing columns {missing}. Found: {list(reader.fieldnames)}"
            )
        for i, row in enumerate(reader, start=2):
            try:
                st = float(row["sys_time"].strip())
                ut = float(row["usr_time"].strip())
                pw = float(row[target_col].strip())
            except (KeyError, ValueError, AttributeError) as e:
                raise SystemExit(f"{label} row {i}: invalid or empty numeric field ({e})") from e
            if not all(math.isfinite(v) for v in (st, ut, pw)):
                continue
            xs.append([st, ut])
            ys.append(pw)
    if len(xs) < min_rows:
        raise SystemExit(
            f"{label}: need at least {min_rows} valid rows after filtering; got {len(xs)}"
        )
    return np.asarray(xs, dtype=np.float64), np.asarray(ys, dtype=np.float64)


def _rmse(y_true: np.ndarray, y_pred: np.ndarray) -> float:
    return float(math.sqrt(mean_squared_error(y_true, y_pred)))


def _print_metrics(y_true: np.ndarray, y_pred: np.ndarray, indent: str = "  ") -> None:
    print(f"{indent}R²:   {r2_score(y_true, y_pred):.6g}")
    print(f"{indent}MAE:  {mean_absolute_error(y_true, y_pred):.6g}")
    print(f"{indent}RMSE: {_rmse(y_true, y_pred):.6g}")


def main() -> None:
    p = argparse.ArgumentParser(
        description="Linear regression power_watt ~ sys_time + usr_time with k-fold CV."
    )
    p.add_argument("--csv", type=Path, required=True, help="Training CSV path")
    p.add_argument(
        "--test-csv",
        type=Path,
        default=None,
        metavar="PATH",
        help="Optional held-out test CSV (same columns as training data)",
    )
    p.add_argument(
        "--target",
        default=DEFAULT_TARGET,
        metavar="COL",
        help=f"Target column name (default: {DEFAULT_TARGET})",
    )
    p.add_argument("--folds", type=int, default=5, help="Number of CV folds (default: 5)")
    p.add_argument(
        "--seed",
        type=int,
        default=42,
        help="Random seed for shuffled K-fold (default: 42)",
    )
    args = p.parse_args()

    if args.folds < 2:
        raise SystemExit("--folds must be at least 2")

    X, y = _load_rows(args.csv, args.target, label="Training CSV")
    n = len(y)

    if n < args.folds:
        raise SystemExit(f"Need at least as many samples as folds ({args.folds}); got {n}")

    model = LinearRegression()
    cv = KFold(n_splits=args.folds, shuffle=True, random_state=args.seed)
    rmse_scorer = make_scorer(_rmse, greater_is_better=False)

    scores = cross_validate(
        model,
        X,
        y,
        cv=cv,
        scoring={
            "r2": "r2",
            "neg_mae": "neg_mean_absolute_error",
            "rmse": rmse_scorer,
        },
        return_train_score=False,
    )

    def summarize(name: str, arr: np.ndarray) -> str:
        m = float(np.mean(arr))
        s = float(np.std(arr))
        return f"{name}: mean={m:.6g}  std={s:.6g}"

    print(f"Samples: {n}  Features: {list(FEATURES)}  Target: {args.target}")
    print(f"Cross-validation: {args.folds}-fold (shuffled, seed={args.seed})")
    print(summarize("R² (per fold)", scores["test_r2"]))
    print(summarize("MAE (per fold)", -scores["test_neg_mae"]))
    # Custom loss scorers are negated when greater_is_better=False
    print(summarize("RMSE (per fold)", -scores["test_rmse"]))

    model.fit(X, y)
    y_hat = model.predict(X)
    print("\nFull-data fit (reference; not held-out):")
    _print_metrics(y, y_hat)
    print(f"  Intercept: {model.intercept_:.6g}")
    for name, coef in zip(FEATURES, model.coef_, strict=True):
        print(f"  Coef {name}: {coef:.6g}")

    if args.test_csv is not None:
        X_test, y_test = _load_rows(
            args.test_csv, args.target, min_rows=1, label="Test CSV"
        )
        y_test_hat = model.predict(X_test)
        print(f"\nHeld-out test set ({args.test_csv}, n={len(y_test)}):")
        _print_metrics(y_test, y_test_hat)


if __name__ == "__main__":
    main()
