package stats

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/sustainable-computing-io/kepler/pkg/collector/stats/types"
	"github.com/sustainable-computing-io/kepler/pkg/config"
)

var _ = Describe("ToEstimatorValues expression", func() {
	It("evaluates linear resource usage expression like CPUTime+2*CPUCycle", func() {
		// Save/restore global to avoid leaking into other tests.
		orig := config.SamplePeriodSec
		config.SamplePeriodSec = 10
		defer func() { config.SamplePeriodSec = orig }()

		s := &Stats{
			ResourceUsage: map[string]types.UInt64StatCollection{},
			EnergyUsage:   map[string]types.UInt64StatCollection{},
		}
		s.ResourceUsage[config.CPUTime] = types.NewUInt64StatCollection()
		s.ResourceUsage[config.CPUCycle] = types.NewUInt64StatCollection()

		// Any key is fine: ToEstimatorValues uses SumAllDeltaValues across all sources.
		s.ResourceUsage[config.CPUTime].SetDeltaStat("0", 100) // delta over SamplePeriodSec
		s.ResourceUsage[config.CPUCycle].SetDeltaStat("0", 50) // delta over SamplePeriodSec

		expr := config.CPUTime + " + 2*" + config.CPUCycle
		vals := s.ToEstimatorValues([]string{expr}, true)
		Expect(vals).To(HaveLen(1))

		// normalize by SamplePeriodSec
		Expect(vals[0]).To(BeNumerically("==", float64(100+2*50)/float64(config.SamplePeriodSec)))
	})
})
