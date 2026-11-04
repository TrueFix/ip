// Package buildinfo exposes non-secret release metadata injected at build time.
package buildinfo

import (
	"fmt"
	"runtime"
)

var (
	Version   = "dev"
	Revision  = "unknown"
	BuildDate = "unknown"
)

type Info struct {
	Version   string `json:"version"`
	Revision  string `json:"revision"`
	BuildDate string `json:"build_date"`
	GoVersion string `json:"go_version"`
}

func Current() Info {
	return Info{Version: Version, Revision: Revision, BuildDate: BuildDate, GoVersion: runtime.Version()}
}
func String() string {
	return fmt.Sprintf("myip %s (revision=%s built=%s go=%s)", Version, Revision, BuildDate, runtime.Version())
}
