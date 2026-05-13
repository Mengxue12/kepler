// SPDX-FileCopyrightText: 2025 The Kepler Authors
// SPDX-License-Identifier: Apache-2.0

// Estimator sidecar: Unix stream socket JSON API compatible with Kepler's node power estimator
// client (request "{}\n", response {"power_watts":<float>}\n). Reads /proc/stat aggregate cpu
// jiffies between requests and applies a configurable linear model.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	socketPath := getenv("ESTIMATOR_SOCKET", "/tmp/estimator.sock")
	procRoot := getenv("ESTIMATOR_PROC", "/proc")
	coeffPath := os.Getenv("ESTIMATOR_COEFFICIENTS")
	metricsAddr := os.Getenv("ESTIMATOR_METRICS_LISTEN")

	logLevel := slog.LevelInfo
	if os.Getenv("ESTIMATOR_LOG_DEBUG") == "1" {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))

	model, err := loadModel(coeffPath)
	if err != nil {
		logger.Error("load coefficients", "path", coeffPath, "error", err)
		os.Exit(1)
	}

	srv := newServer(logger, procRoot, model)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if metricsAddr != "" {
		go func() {
			logger.Info("metrics server listening", "addr", metricsAddr)
			if err := srv.ServeMetrics(metricsAddr); err != nil {
				logger.Error("metrics server", "error", err)
			}
		}()
	}

	logger.Info("unix estimator listening", "socket", socketPath, "proc", procRoot)
	if err := runUnixServer(ctx, logger, srv, socketPath); err != nil {
		logger.Error("unix server", "error", err)
		os.Exit(1)
	}
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
