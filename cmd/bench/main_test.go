package main

import (
	"testing"
	"time"
)

func TestSummarize(t *testing.T) {
	s := summarize([]time.Duration{5 * time.Microsecond, time.Microsecond, 3 * time.Microsecond, 2 * time.Microsecond, 4 * time.Microsecond})
	if s.MeanUS != 3 || s.P50US != 3 || s.P95US != 5 || s.P99US != 5 || s.Samples != 5 {
		t.Fatal(s)
	}
}
