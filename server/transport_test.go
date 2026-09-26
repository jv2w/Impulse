package server

import (
	"strings"
	"testing"
)

// TestParsePortRange pins the "port" and "min-max" forms and their failures: a
// typo in UDPPorts must error instead of listening on an unintended port.
func TestParsePortRange(t *testing.T) {
	for _, c := range []struct {
		in       string
		min, max uint16
	}{
		{"", 0, 0},
		{"19133", 19133, 19133},
		{"19133-19134", 19133, 19134},
	} {
		r, err := parsePortRange(c.in)
		if err != nil {
			t.Fatalf("parsePortRange(%q) errored: %v", c.in, err)
		}
		if r.Min != c.min || r.Max != c.max {
			t.Fatalf("parsePortRange(%q) = %+v, want {%d %d}", c.in, r, c.min, c.max)
		}
	}
	for _, in := range []string{"abc", "19133-", "-19134", "19134-19133", "99999"} {
		if r, err := parsePortRange(in); err == nil {
			t.Fatalf("parsePortRange(%q) = %+v, want an error", in, r)
		}
	}
}

// TestTransportListenersNames pins the transport mapping: empty defaults to the
// single RakNet listener, duplicates listen once, and an unknown name errors
// instead of leaving the server silently unreachable.
func TestTransportListenersNames(t *testing.T) {
	var uc UserConfig
	listeners, err := uc.transportListeners()
	if err != nil {
		t.Fatalf("default transportListeners() errored: %v", err)
	}
	if len(listeners) != 1 {
		t.Fatalf("default transportListeners() = %d listeners, want 1", len(listeners))
	}

	uc.Network.Transport = []string{"raknet", "nethernet", "raknet"}
	if listeners, err = uc.transportListeners(); err != nil {
		t.Fatalf("transportListeners() errored: %v", err)
	} else if len(listeners) != 2 {
		t.Fatalf("transportListeners() = %d listeners, want 2", len(listeners))
	}

	uc.Network.Transport = []string{"bogus"}
	if _, err = uc.transportListeners(); err == nil || !strings.Contains(err.Error(), `unknown network transport "bogus"`) {
		t.Fatalf("transportListeners() error = %v, want unknown network transport", err)
	}
}
