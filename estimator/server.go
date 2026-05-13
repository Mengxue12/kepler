// SPDX-FileCopyrightText: 2025 The Kepler Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
)

type estimatorServer struct {
	logger   *slog.Logger
	procRoot string
	model    LinearModel

	mu       sync.Mutex
	prev     CPUJiffies
	prevInit bool

	lastDUser   atomic.Uint64
	lastDSystem atomic.Uint64
	lastPowerW  atomic.Uint64
}

func newServer(logger *slog.Logger, procRoot string, model LinearModel) *estimatorServer {
	return &estimatorServer{logger: logger, procRoot: procRoot, model: model}
}

func (s *estimatorServer) lastPowerFloat() float64 {
	return math.Float64frombits(s.lastPowerW.Load())
}

// ServeMetrics exposes last deltas and predicted power for Prometheus scraping.
func (s *estimatorServer) ServeMetrics(addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		p := s.lastPowerFloat()
		_, _ = fmt.Fprintf(w, "# HELP estimator_power_watts_last Predicted platform power from linear model (last sample).\n")
		_, _ = fmt.Fprintf(w, "# TYPE estimator_power_watts_last gauge\n")
		_, _ = fmt.Fprintf(w, "estimator_power_watts_last %g\n", p)
		_, _ = fmt.Fprintf(w, "# HELP estimator_procstat_user_jiffies_delta Last interval delta user jiffies (aggregate cpu line).\n")
		_, _ = fmt.Fprintf(w, "# TYPE estimator_procstat_user_jiffies_delta gauge\n")
		_, _ = fmt.Fprintf(w, "estimator_procstat_user_jiffies_delta %d\n", s.lastDUser.Load())
		_, _ = fmt.Fprintf(w, "# HELP estimator_procstat_system_jiffies_delta Last interval delta system jiffies.\n")
		_, _ = fmt.Fprintf(w, "# TYPE estimator_procstat_system_jiffies_delta gauge\n")
		_, _ = fmt.Fprintf(w, "estimator_procstat_system_jiffies_delta %d\n", s.lastDSystem.Load())
	})
	srv := &http.Server{Addr: addr, Handler: mux}
	return srv.ListenAndServe()
}

func (s *estimatorServer) handleConn(c net.Conn) {
	defer func() { _ = c.Close() }()
	br := bufio.NewReader(c)
	_, err := br.ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		s.logger.Debug("read request", "error", err)
		return
	}

	pw, err := s.predictOnce()
	if err != nil {
		s.logger.Warn("predict failed", "error", err)
		return
	}
	s.lastPowerW.Store(math.Float64bits(pw))
	resp := map[string]float64{"power_watts": pw}
	line, err := json.Marshal(resp)
	if err != nil {
		return
	}
	if _, err := c.Write(append(line, '\n')); err != nil {
		s.logger.Debug("write response", "error", err)
	}
}

func (s *estimatorServer) predictOnce() (float64, error) {
	cur, err := ReadCPUStat(s.procRoot)
	if err != nil {
		return 0, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.prevInit {
		s.prev = cur
		s.prevInit = true
		s.lastDUser.Store(0)
		s.lastDSystem.Store(0)
		return s.model.predictWatts(0, 0), nil
	}

	dUser, dSystem := subJiffies(cur, s.prev)
	s.prev = cur
	s.lastDUser.Store(dUser)
	s.lastDSystem.Store(dSystem)

	return s.model.predictWatts(dUser, dSystem), nil
}

func runUnixServer(ctx context.Context, logger *slog.Logger, srv *estimatorServer, socketPath string) error {
	_ = os.Remove(socketPath)
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("listen unix %s: %w", socketPath, err)
	}
	defer func() { _ = ln.Close() }()

	if err := os.Chmod(socketPath, 0o660); err != nil {
		logger.Warn("chmod socket", "path", socketPath, "error", err)
	}

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("accept: %w", err)
		}
		go srv.handleConn(c)
	}
}
