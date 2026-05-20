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
	"sort"
	"sync"
)

type estimatorServer struct {
	logger   *slog.Logger
	procRoot string
	nodeName string
	model    LinearModel

	mu       sync.Mutex
	prev     CPUJiffies
	prevInit bool

	lastDeltas map[string]uint64
	lastPowerW uint64
}

func newServer(logger *slog.Logger, procRoot, nodeName string, model LinearModel) *estimatorServer {
	return &estimatorServer{
		logger:     logger,
		procRoot:   procRoot,
		nodeName:   nodeName,
		model:      model,
		lastDeltas: make(map[string]uint64),
	}
}

func (s *estimatorServer) lastPowerFloat() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return math.Float64frombits(s.lastPowerW)
}

// ServeMetrics exposes last deltas and predicted power for Prometheus scraping.
func (s *estimatorServer) ServeMetrics(addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		p := s.lastPowerFloat()
		s.mu.Lock()
		deltas := make(map[string]uint64, len(s.lastDeltas))
		timeTypes := make([]string, 0, len(s.lastDeltas))
		for k, v := range s.lastDeltas {
			deltas[k] = v
			timeTypes = append(timeTypes, k)
		}
		s.mu.Unlock()
		sort.Strings(timeTypes)

		_, _ = fmt.Fprintf(w, "# HELP estimator_power_watts_last Predicted platform power from linear model (last sample).\n")
		_, _ = fmt.Fprintf(w, "# TYPE estimator_power_watts_last gauge\n")
		_, _ = fmt.Fprintf(w, "estimator_power_watts_last %g\n", p)
		_, _ = fmt.Fprintf(w, "# HELP estimator_procstat_jiffies_delta Last interval delta jiffies by time type from aggregate cpu line.\n")
		_, _ = fmt.Fprintf(w, "# TYPE estimator_procstat_jiffies_delta gauge\n")
		for _, timeType := range timeTypes {
			_, _ = fmt.Fprintf(w, "estimator_procstat_jiffies_delta{mode=%q,node_name=%q} %d\n", timeType, s.nodeName, deltas[timeType])
		}
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
	s.mu.Lock()
	s.lastPowerW = math.Float64bits(pw)
	s.mu.Unlock()
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
		s.lastDeltas = make(map[string]uint64, len(cur.Times))
		for timeType := range cur.Times {
			s.lastDeltas[timeType] = 0
		}
		return s.model.predictWatts(0, 0), nil
	}

	deltas := subJiffies(cur, s.prev)
	s.prev = cur
	s.lastDeltas = deltas

	dUser := deltas["user"]
	dSystem := deltas["system"]
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
