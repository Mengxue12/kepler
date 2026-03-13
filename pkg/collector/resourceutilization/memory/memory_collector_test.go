package memory

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sustainable-computing-io/kepler/pkg/bpf"
	"github.com/sustainable-computing-io/kepler/pkg/collector/stats"
	"github.com/sustainable-computing-io/kepler/pkg/config"
	"github.com/sustainable-computing-io/kepler/pkg/utils"
)

func TestUpdateNodeMemoryBandwidthMetrics(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "vmstat")

	origPath := vmStatPath
	defer func() { vmStatPath = origPath }()
	vmStatPath = path

	prevNodeMemReadBytes = 0
	prevNodeMemWriteBytes = 0

	nodeStats := stats.NewNodeStats(bpf.DefaultSupportedMetrics())

	if err := os.WriteFile(path, []byte("pgpgin 100\npgpgout 200\n"), 0o644); err != nil {
		t.Fatalf("write first vmstat: %v", err)
	}
	UpdateNodeMemoryBandwidthMetrics(nodeStats)

	read1 := nodeStats.ResourceUsage[config.MemRead][utils.GenericSocketID].GetDelta()
	write1 := nodeStats.ResourceUsage[config.MemWrite][utils.GenericSocketID].GetDelta()
	if read1 != 100*1024 {
		t.Fatalf("unexpected mem_read delta: got=%d want=%d", read1, uint64(100*1024))
	}
	if write1 != 200*1024 {
		t.Fatalf("unexpected mem_write delta: got=%d want=%d", write1, uint64(200*1024))
	}

	nodeStats.ResetDeltaValues()
	if err := os.WriteFile(path, []byte("pgpgin 180\npgpgout 260\n"), 0o644); err != nil {
		t.Fatalf("write second vmstat: %v", err)
	}
	UpdateNodeMemoryBandwidthMetrics(nodeStats)

	read2 := nodeStats.ResourceUsage[config.MemRead][utils.GenericSocketID].GetDelta()
	write2 := nodeStats.ResourceUsage[config.MemWrite][utils.GenericSocketID].GetDelta()
	if read2 != 80*1024 {
		t.Fatalf("unexpected second mem_read delta: got=%d want=%d", read2, uint64(80*1024))
	}
	if write2 != 60*1024 {
		t.Fatalf("unexpected second mem_write delta: got=%d want=%d", write2, uint64(60*1024))
	}
}
