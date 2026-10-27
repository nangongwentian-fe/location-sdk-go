package locationsdk

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sync/atomic"
	"time"
)

// Stats is a cumulative, point-free diagnostic snapshot for host monitoring.
type Stats struct {
	Calls               uint64 `json:"calls"`
	Errors              uint64 `json:"errors"`
	Timeouts            uint64 `json:"timeouts"`
	DurationNanoseconds uint64 `json:"durationNanoseconds"`
}
type counters struct{ calls, errors, timeouts, duration atomic.Uint64 }

func (c *Client) Stats() Stats {
	return Stats{Calls: c.metrics.calls.Load(), Errors: c.metrics.errors.Load(), Timeouts: c.metrics.timeouts.Load(), DurationNanoseconds: c.metrics.duration.Load()}
}
func (c *Client) recordCall(elapsed time.Duration, err error) {
	c.metrics.calls.Add(1)
	c.metrics.duration.Add(uint64(elapsed))
	if err != nil {
		c.metrics.errors.Add(1)
	}
	if status.Code(err) == codes.DeadlineExceeded {
		c.metrics.timeouts.Add(1)
	}
}
