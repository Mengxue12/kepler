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

package network

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/sustainable-computing-io/kepler/pkg/collector/stats"
	"github.com/sustainable-computing-io/kepler/pkg/config"
	"k8s.io/klog/v2"
)

var (
	mx sync.Mutex

	// Previous cumulative /proc/<pid>/net/dev values by container ID and netns/interface.
	prevContainerRxBytes = map[string]map[string]uint64{}
	prevContainerTxBytes = map[string]map[string]uint64{}

	// Previous cumulative /proc/net/dev values by netns/interface.
	prevNodeRxBytes = map[string]uint64{}
	prevNodeTxBytes = map[string]uint64{}
)

func UpdateContainerNetworkMetrics(containerStats map[string]*stats.ContainerStats) {
	mx.Lock()
	defer mx.Unlock()

	for containerID, cStat := range containerStats {
		pids, pidNetNSMap, ok := getOnePIDPerNetNS(cStat)
		if !ok {
			continue
		}
		klog.V(6).Infof("pod/container %s/%s %s selected pids[network namespace]: %v", cStat.PodName, cStat.ContainerName, containerID, pidNetNSMap)

		currRxByID := map[string]uint64{}
		currTxByID := map[string]uint64{}
		reads := 0
		for _, pid := range pids {
			netNSID := pidNetNSMap[pid]
			perInterfaceStats, err := getNetDevStats(fmt.Sprintf("/proc/%d/net/dev", pid), netNSID, true)
			if err != nil {
				klog.V(6).Infof("failed to read net stats for container %s pid %d: %v", containerID, pid, err)
				continue
			}
			for usageID, io := range perInterfaceStats {
				currRxByID[usageID] += io.rx
				currTxByID[usageID] += io.tx
			}
			reads++
		}
		if reads == 0 || len(currRxByID) == 0 {
			continue
		}

		if _, exists := prevContainerRxBytes[containerID]; !exists {
			prevContainerRxBytes[containerID] = map[string]uint64{}
		}
		if _, exists := prevContainerTxBytes[containerID]; !exists {
			prevContainerTxBytes[containerID] = map[string]uint64{}
		}

		for usageID, currRx := range currRxByID {
			prevRx := prevContainerRxBytes[containerID][usageID]
			prevContainerRxBytes[containerID][usageID] = currRx
			if currRx >= prevRx {
				cStat.ResourceUsage[config.NetRX].AddDeltaStat(usageID, currRx-prevRx)
			}
		}
		for usageID, currTx := range currTxByID {
			prevTx := prevContainerTxBytes[containerID][usageID]
			prevContainerTxBytes[containerID][usageID] = currTx
			if currTx >= prevTx {
				cStat.ResourceUsage[config.NetTX].AddDeltaStat(usageID, currTx-prevTx)
			}
		}
	}
}

func UpdateNodeNetworkMetrics(nodeStats *stats.NodeStats) {
	mx.Lock()
	defer mx.Unlock()

	nodeNetNS, ok := getNetNSID(1)
	if !ok {
		nodeNetNS = "unknown"
	}
	perInterfaceStats, err := getNetDevStats("/proc/net/dev", nodeNetNS, true)
	if err != nil {
		klog.V(3).Infof("failed to read /proc/net/dev: %v", err)
		return
	}

	for usageID, io := range perInterfaceStats {
		prevRx := prevNodeRxBytes[usageID]
		if io.rx >= prevRx {
			nodeStats.ResourceUsage[config.NetRX].AddDeltaStat(usageID, io.rx-prevRx)
		} else {
			klog.V(6).Infof("node network RX bytes decreased for %s: %d -> %d", usageID, prevRx, io.rx)
		}
		prevNodeRxBytes[usageID] = io.rx
	}
	for usageID, io := range perInterfaceStats {
		prevTx := prevNodeTxBytes[usageID]
		if io.tx >= prevTx {
			nodeStats.ResourceUsage[config.NetTX].AddDeltaStat(usageID, io.tx-prevTx)
		} else {
			klog.V(6).Infof("node network TX bytes decreased for %s: %d -> %d", usageID, prevTx, io.tx)
		}
		prevNodeTxBytes[usageID] = io.tx
	}
}

func getOnePIDPerNetNS(cStat *stats.ContainerStats) ([]uint64, map[uint64]string, bool) {
	pidByNetNS := map[string]uint64{}
	for pid := range cStat.PIDS {
		netNSID, ok := getNetNSID(pid)
		if !ok {
			continue
		}
		if existingPID, exists := pidByNetNS[netNSID]; !exists || pid < existingPID {
			pidByNetNS[netNSID] = pid
		}
	}
	if len(pidByNetNS) == 0 {
		return nil, nil, false
	}
	pids := make([]uint64, 0, len(pidByNetNS))
	pidNetNSMap := make(map[uint64]string, len(pidByNetNS))
	for _, pid := range pidByNetNS {
		pids = append(pids, pid)
	}
	for netNSID, pid := range pidByNetNS {
		pidNetNSMap[pid] = netNSID
	}
	sort.Slice(pids, func(i, j int) bool { return pids[i] < pids[j] })
	return pids, pidNetNSMap, true
}

func getNetNSID(pid uint64) (string, bool) {
	netNSLink, err := os.Readlink(fmt.Sprintf("/proc/%d/ns/net", pid))
	if err != nil {
		return "", false
	}
	return netNSLink, true
}

type netDevCounter struct {
	rx uint64
	tx uint64
}

func getNetDevStats(path, netNSID string, skipLoopback bool) (map[string]netDevCounter, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	result := map[string]netDevCounter{}
	lineNum := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		lineNum++
		// Skip headers in /proc/net/dev.
		if lineNum <= 2 || line == "" {
			continue
		}

		parts := strings.Split(line, ":")
		if len(parts) != 2 {
			continue
		}
		iface := strings.TrimSpace(parts[0])
		if skipLoopback && iface == "lo" {
			continue
		}

		fields := strings.Fields(parts[1])
		// rx bytes is field 0, tx bytes is field 8.
		if len(fields) < 9 {
			continue
		}

		rx, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		tx, err := strconv.ParseUint(fields[8], 10, 64)
		if err != nil {
			continue
		}
		usageID := getNetUsageID(netNSID, iface)
		result[usageID] = netDevCounter{rx: rx, tx: tx}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func getNetUsageID(netNSID, iface string) string {
	return netNSID + "|" + iface
}
