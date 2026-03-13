/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package memory

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/sustainable-computing-io/kepler/pkg/collector/stats"
	"github.com/sustainable-computing-io/kepler/pkg/config"
	"github.com/sustainable-computing-io/kepler/pkg/utils"
	"k8s.io/klog/v2"
)

var (
	mx sync.Mutex

	prevNodeMemReadBytes  uint64
	prevNodeMemWriteBytes uint64

	vmStatPath = "/proc/vmstat"
)

// UpdateNodeMemoryBandwidthMetrics updates node memory counters in bytes.
// Data source:
//   - /proc/vmstat pgpgin  -> cumulative bytes read
//   - /proc/vmstat pgpgout -> cumulative bytes written
func UpdateNodeMemoryBandwidthMetrics(nodeStats *stats.NodeStats) {
	mx.Lock()
	defer mx.Unlock()

	readBytes, writeBytes, err := getNodeMemoryBytes()
	if err != nil {
		klog.V(3).Infof("failed to parse %s for memory bandwidth: %v", vmStatPath, err)
		return
	}

	if readBytes >= prevNodeMemReadBytes {
		nodeStats.ResourceUsage[config.MemRead].AddDeltaStat(utils.GenericSocketID, readBytes-prevNodeMemReadBytes)
	}
	if writeBytes >= prevNodeMemWriteBytes {
		nodeStats.ResourceUsage[config.MemWrite].AddDeltaStat(utils.GenericSocketID, writeBytes-prevNodeMemWriteBytes)
	}

	prevNodeMemReadBytes = readBytes
	prevNodeMemWriteBytes = writeBytes
}

func getNodeMemoryBytes() (uint64, uint64, error) {
	data, err := os.ReadFile(vmStatPath)
	if err != nil {
		return 0, 0, err
	}

	var pgpginKB uint64
	var pgpgoutKB uint64
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		switch fields[0] {
		case "pgpgin":
			v, err := strconv.ParseUint(fields[1], 10, 64)
			if err != nil {
				return 0, 0, fmt.Errorf("invalid pgpgin value: %w", err)
			}
			pgpginKB = v
		case "pgpgout":
			v, err := strconv.ParseUint(fields[1], 10, 64)
			if err != nil {
				return 0, 0, fmt.Errorf("invalid pgpgout value: %w", err)
			}
			pgpgoutKB = v
		}
	}

	return pgpginKB * 1024, pgpgoutKB * 1024, nil
}
