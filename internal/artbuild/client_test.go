package artbuild

import (
	"testing"
	"time"
)

func TestHistoryURL(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	for nvr, want := range map[string]string{
		"quay-operator-container-3.16.7-202610130021.p2.ge44ea81.assembly.stream.el9":            "https://art.example/?dateRange=2026-09-13+to+2026-10-13&group=quay-3.16&nvr=quay-operator-container-%2A",
		"quay-operator-metadata-container-3.18.2.202610130038.p2.g117e757.assembly.stream.el9-1": "https://art.example/?dateRange=2026-09-13+to+2026-10-13&group=quay-3.18&nvr=quay-operator-metadata-container-%2A",
		"a/quay-nvr": "",
	} {
		if got := HistoryURL("https://art.example/", nvr, now); got != want {
			t.Errorf("HistoryURL(%q) = %q, want %q", nvr, got, want)
		}
	}
}
