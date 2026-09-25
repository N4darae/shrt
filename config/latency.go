package config

import "fmt"

const (
	DefaultLatencyFloorMS   = 250
	DefaultLatencyRatio     = 3.0
	DefaultLatencyRemeasure = 2
)

type Latency struct {
	FloorMS   *int64   `yaml:"floor_ms,omitempty"`
	Ratio     *float64 `yaml:"ratio,omitempty"`
	Remeasure *int     `yaml:"remeasure,omitempty"`
	Fail      bool     `yaml:"fail,omitempty"`
	Off       bool     `yaml:"off,omitempty"`
}

func (l *Latency) validate() error {
	if l == nil {
		return nil
	}
	if l.FloorMS != nil && *l.FloorMS < 0 {
		return fmt.Errorf("latency.floor_ms: %d is negative; it is how many milliseconds slower than the safe spot's run a step must be before it is flagged (default %d)", *l.FloorMS, DefaultLatencyFloorMS)
	}
	if l.Ratio != nil && *l.Ratio < 1 {
		return fmt.Errorf("latency.ratio: %g is below 1; it is how many times the safe spot's latency a step must take before it is flagged (default %g)", *l.Ratio, DefaultLatencyRatio)
	}
	if l.Remeasure != nil && (*l.Remeasure < 0 || *l.Remeasure > 10) {
		return fmt.Errorf("latency.remeasure: %d is outside 0..10; it is how many more times a slow read is re-sent before it is judged (default %d)", *l.Remeasure, DefaultLatencyRemeasure)
	}
	return nil
}
