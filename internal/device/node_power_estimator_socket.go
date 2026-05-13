// SPDX-FileCopyrightText: 2025 The Kepler Authors
// SPDX-License-Identifier: Apache-2.0

package device

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"time"
)

// Line-oriented JSON over stream: Kepler sends "{}\n"; sidecar responds with one JSON object and '\n'.

const (
	maxEstimatorSocketLineBytes = 65536
	maxEstimatorPowerWatts      = 1e7 // reject absurd responses
)

type estimatorSocketResponse struct {
	PowerWatts      *float64 `json:"power_watts"`
	PowerMilliWatts *float64 `json:"power_milliwatts"`
}

func queryEstimatorPowerUnix(path string, timeout time.Duration) (Power, error) {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	conn, err := net.DialTimeout("unix", path, timeout)
	if err != nil {
		return 0, err
	}
	defer func() { _ = conn.Close() }()

	deadline := time.Now().Add(timeout)
	if err := conn.SetDeadline(deadline); err != nil {
		return 0, err
	}
	if _, err := io.WriteString(conn, "{}\n"); err != nil {
		return 0, fmt.Errorf("write request: %w", err)
	}

	lr := io.LimitReader(conn, maxEstimatorSocketLineBytes+1)
	br := bufio.NewReader(lr)
	line, err := br.ReadBytes('\n')
	if err != nil {
		return 0, fmt.Errorf("read response: %w", err)
	}
	if len(line) > maxEstimatorSocketLineBytes {
		return 0, fmt.Errorf("response line exceeds %d bytes", maxEstimatorSocketLineBytes)
	}

	var resp estimatorSocketResponse
	if err := json.Unmarshal(bytes.TrimSpace(line), &resp); err != nil {
		return 0, fmt.Errorf("parse json: %w", err)
	}
	return responseToPower(&resp)
}

func responseToPower(r *estimatorSocketResponse) (Power, error) {
	if r.PowerWatts != nil {
		w := *r.PowerWatts
		if math.IsNaN(w) || math.IsInf(w, 0) || w <= 0 || w > maxEstimatorPowerWatts {
			return 0, fmt.Errorf("invalid power_watts: %v", w)
		}
		return Power(w * float64(Watt)), nil
	}
	if r.PowerMilliWatts != nil {
		mw := *r.PowerMilliWatts
		if math.IsNaN(mw) || math.IsInf(mw, 0) || mw <= 0 || mw > maxEstimatorPowerWatts*1000 {
			return 0, fmt.Errorf("invalid power_milliwatts: %v", mw)
		}
		return Power(mw * float64(MilliWatt)), nil
	}
	return 0, fmt.Errorf("missing power_watts or power_milliwatts")
}
