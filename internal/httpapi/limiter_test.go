package httpapi

import (
	"net/netip"
	"testing"
	"time"
)

func TestRollingMinuteBudget(t *testing.T) {
	l := NewLimiter(3, 0, 20, 10)
	ip := netip.MustParseAddr("8.8.8.8")
	now := time.Now()
	for i := range 3 {
		if !l.Allow(ip, now.Add(time.Duration(i)*time.Second)) {
			t.Fatal("early denial")
		}
	}
	if ok, retry := l.AllowWithRetry(ip, now.Add(59*time.Second)); ok || retry != time.Second {
		t.Fatalf("rolling budget bypass or incorrect retry: %v %s", ok, retry)
	}
	if !l.Allow(ip, now.Add(time.Minute)) {
		t.Fatal("request did not expire at 60 seconds")
	}
	if l.Allow(ip, now.Add(time.Minute)) {
		t.Fatal("more than one expired slot reused")
	}
}

func TestZeroRPMDisablesAllLimits(t *testing.T) {
	l := NewLimiter(0, 1, 1, 1)
	now := time.Now()
	for _, address := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		for range 1000 {
			if allowed, retry := l.AllowWithRetry(netip.MustParseAddr(address), now); !allowed || retry != 0 {
				t.Fatalf("disabled limiter denied a request: %v %s", allowed, retry)
			}
		}
	}
	if stats := l.Stats(); stats != (LimitStats{}) {
		t.Fatalf("disabled limiter tracked clients or denials: %+v", stats)
	}
}

func TestRollingRingGrowthAndWrap(t *testing.T) {
	l := NewLimiter(35, 0, 1, 10)
	ip := netip.MustParseAddr("8.8.8.8")
	now := time.Now()
	for minute := range 5 {
		for i := range 35 {
			if !l.Allow(ip, now.Add(time.Duration(minute)*time.Minute+time.Duration(i)*time.Millisecond)) {
				t.Fatal("ring request denied")
			}
		}
		if l.Allow(ip, now.Add(time.Duration(minute)*time.Minute+time.Second)) {
			t.Fatal("minute quota exceeded")
		}
	}
	if len(l.clients[ip].stamps) > 35 {
		t.Fatal("timestamp allocation exceeded budget")
	}
}

func TestOptionalRPSDoesNotConsumeRPMOnDenial(t *testing.T) {
	l := NewLimiter(2, 1, 1, 10)
	ip := netip.MustParseAddr("8.8.8.8")
	now := time.Now()
	if !l.Allow(ip, now) {
		t.Fatal("initial denial")
	}
	if l.Allow(ip, now) {
		t.Fatal("RPS burst bypass")
	}
	if !l.Allow(ip, now.Add(time.Second)) {
		t.Fatal("RPS denial consumed RPM")
	}
	if l.Allow(ip, now.Add(2*time.Second)) {
		t.Fatal("RPM bypassed by RPS token refill")
	}
}

func TestRateLimitStatsAreProcessLocal(t *testing.T) {
	l := NewLimiter(1, 0, 1, 1)
	ip := netip.MustParseAddr("8.8.8.8")
	now := time.Now()
	l.Allow(ip, now)
	l.Allow(ip, now)
	l.Allow(netip.MustParseAddr("1.1.1.1"), now)
	stats := l.Stats()
	if stats.Clients != 1 || stats.DeniedRPM != 1 || stats.DeniedCapacity != 1 {
		t.Fatal(stats)
	}
	fresh := NewLimiter(1, 0, 1, 1)
	if !fresh.Allow(ip, now) || fresh.Stats().DeniedRPM != 0 {
		t.Fatal("restart semantics changed")
	}
}
