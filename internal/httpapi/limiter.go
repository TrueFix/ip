package httpapi

import (
	"golang.org/x/time/rate"
	"net/netip"
	"sync"
	"time"
)

type clientBucket struct {
	limiter     *rate.Limiter
	seen        time.Time
	stamps      []int64
	head, count int
}
type Limiter struct {
	disabled                             bool
	mu                                   sync.Mutex
	clients                              map[netip.Addr]*clientBucket
	rps                                  rate.Limit
	rpm, burst, capacity                 int
	lastSweep                            time.Time
	epoch                                time.Time
	deniedRPM, deniedRPS, deniedCapacity uint64
}

func NewLimiter(rpm int, rps float64, burst, capacity int) *Limiter {
	return &Limiter{clients: make(map[netip.Addr]*clientBucket), rpm: rpm, rps: rate.Limit(rps), burst: burst, capacity: capacity}
}
func (l *Limiter) Allow(ip netip.Addr, now time.Time) bool {
	ok, _ := l.AllowWithRetry(ip, now)
	return ok
}

// RPM is a strict rolling-minute budget. Optional RPS is an additional token
// bucket with configurable burst, not a replacement for the minute budget.
func (l *Limiter) AllowWithRetry(ip netip.Addr, now time.Time) (bool, time.Duration) {
	// Only the explicit enable switch bypasses limiting and client tracking.
	if l.disabled {
		return true, 0
	}
	if ip.Is6() {
		ip = netip.PrefixFrom(ip, 64).Masked().Addr()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.rpm == 0 {
		l.deniedRPM++
		return false, time.Minute
	}
	if l.epoch.IsZero() {
		l.epoch = now
	}
	if now.Sub(l.lastSweep) > time.Minute {
		for key, v := range l.clients {
			if now.Sub(v.seen) > 10*time.Minute {
				delete(l.clients, key)
			}
		}
		l.lastSweep = now
	}
	v, ok := l.clients[ip]
	if !ok {
		if len(l.clients) >= l.capacity {
			l.deniedCapacity++
			return false, time.Minute
		}
		v = &clientBucket{stamps: make([]int64, min(l.rpm, 16))}
		if l.rps > 0 {
			v.limiter = rate.NewLimiter(l.rps, l.burst)
		}
		l.clients[ip] = v
	}
	v.seen = now
	stamp := now.Sub(l.epoch).Nanoseconds()
	cutoff := stamp - int64(time.Minute)
	for v.count > 0 && v.stamps[v.head] <= cutoff {
		v.head = (v.head + 1) % len(v.stamps)
		v.count--
	}
	if v.count >= l.rpm {
		l.deniedRPM++
		return false, time.Duration(v.stamps[v.head] - cutoff)
	}
	if v.limiter != nil && !v.limiter.AllowN(now, 1) {
		l.deniedRPS++
		wait := time.Duration((1 - v.limiter.TokensAt(now)) / float64(l.rps) * float64(time.Second))
		return false, max(wait, time.Millisecond)
	}
	if v.count == len(v.stamps) {
		grown := make([]int64, min(len(v.stamps)*2, l.rpm))
		for i := range v.count {
			grown[i] = v.stamps[(v.head+i)%len(v.stamps)]
		}
		v.stamps = grown
		v.head = 0
	}
	v.stamps[(v.head+v.count)%len(v.stamps)] = stamp
	v.count++
	return true, 0
}

type LimitStats struct {
	Clients                              int
	DeniedRPM, DeniedRPS, DeniedCapacity uint64
}

func (l *Limiter) Stats() LimitStats {
	l.mu.Lock()
	defer l.mu.Unlock()
	return LimitStats{len(l.clients), l.deniedRPM, l.deniedRPS, l.deniedCapacity}
}
