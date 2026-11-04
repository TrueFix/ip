package httpapi

import (
	"net/netip"
	"sync/atomic"
	"testing"
	"time"
)

// Each worker owns a client and advances its own simulated clock to keep its
// quota available. Includes ring expiry, sweeps, and bucket recreation when one
// worker's clock overtakes another's. Not an HTTP or real-time load benchmark.
func BenchmarkLimiterParallelRolling(b *testing.B) {
	l := NewLimiter(300, 0, 20, 50000)
	base := time.Now()
	var workers atomic.Uint32
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		id := workers.Add(1)
		ip := netip.AddrFrom4([4]byte{11, byte(id >> 16), byte(id >> 8), byte(id)})
		now := base
		for pb.Next() {
			if !l.Allow(ip, now) {
				b.Error("unexpected quota rejection")
				return
			}
			now = now.Add(time.Second)
		}
	})
}

// Exercise the saturated-client rejection path without simulated clock skew.
func BenchmarkLimiterParallelDenied(b *testing.B) {
	l := NewLimiter(300, 0, 20, 50000)
	now := time.Now()
	ip := netip.MustParseAddr("11.0.0.1")
	for i := 0; i < 300; i++ {
		if !l.Allow(ip, now) {
			b.Fatal("failed to populate quota")
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if l.Allow(ip, now) {
				b.Error("unexpected quota acceptance")
				return
			}
		}
	})
}
