package geo

import (
	"fmt"
	"strings"
	"time"
)

// LookupTimings excludes HTTP serialization and network latency. Only consulted
// sources appear in Server-Timing; a skipped fallback is never reported as read.
type LookupTimings struct {
	Total, MaxMind, DBIP, IPInfo            time.Duration
	MaxMindCalled, DBIPCalled, IPInfoCalled bool
}

func (t LookupTimings) ServerTiming() string {
	values := []string{fmt.Sprintf("lookup;dur=%.6f", float64(t.Total)/float64(time.Millisecond))}
	for _, s := range []struct {
		name     string
		duration time.Duration
		called   bool
	}{{"maxmind", t.MaxMind, t.MaxMindCalled}, {"dbip", t.DBIP, t.DBIPCalled}, {"ipinfo", t.IPInfo, t.IPInfoCalled}} {
		if s.called {
			values = append(values, fmt.Sprintf("%s;dur=%.6f", s.name, float64(s.duration)/float64(time.Millisecond)))
		}
	}
	return strings.Join(values, ", ")
}
