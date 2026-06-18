// SPDX-FileCopyrightText: 2025 The Kepler Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
)

// LinearModel predicts platform power (watts) from per-interval deltas of user and system jiffies:
//
//	P = intercept + wU*Δuser + wS*Δsystem
//
// Coefficients are normally fit offline (e.g. sklearn LinearRegression) on labeled data, then saved as JSON.
type LinearModel struct {
	InterceptWatts      float64 `json:"intercept_watts"`
	WattsPerUserJiffy   float64 `json:"watts_per_user_jiffy"`
	WattsPerSystemJiffy float64 `json:"watts_per_system_jiffy"`
	MaxPredictedWatts   float64 `json:"max_predicted_watts"`
	MinPredictedWatts   float64 `json:"min_predicted_watts"`
	maxClamp            float64 `json:"-"`
	minClamp            float64 `json:"-"`
}

func defaultModel() LinearModel {
	return LinearModel{
		InterceptWatts:      2.14276,
		WattsPerUserJiffy:   0.00831919,
		WattsPerSystemJiffy: 0.0,
		maxClamp:            20,
		minClamp:            1,
	}
}

func loadModel(path string) (LinearModel, error) {
	m := defaultModel()
	if path == "" {
		return m, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return LinearModel{}, err
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return LinearModel{}, err
	}
	m.maxClamp = m.MaxPredictedWatts
	if m.maxClamp <= 0 {
		m.maxClamp = 20
	}
	m.minClamp = m.MinPredictedWatts
	if m.minClamp <= 0 {
		m.minClamp = 1
	}
	if math.IsNaN(m.InterceptWatts) || math.IsInf(m.InterceptWatts, 0) {
		return LinearModel{}, fmt.Errorf("invalid intercept_watts")
	}
	return m, nil
}

func (m LinearModel) predictWatts(dUser, dSystem uint64) float64 {
	p := m.InterceptWatts + m.WattsPerUserJiffy*float64(dUser) + m.WattsPerSystemJiffy*float64(dSystem)
	if p < m.minClamp {
		p = m.minClamp
	}
	if p > m.maxClamp {
		p = m.maxClamp
	}
	return p
}
