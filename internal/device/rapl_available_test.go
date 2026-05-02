// SPDX-FileCopyrightText: 2025 The Kepler Authors
// SPDX-License-Identifier: Apache-2.0

package device

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRaplPowercapPresent(t *testing.T) {
	assert.True(t, RaplPowercapPresent("testdata/sys"))
	assert.False(t, RaplPowercapPresent("/nonexistent-sysfs-rapl-test"))
}
