package events

import (
	"net/netip"
	"testing"
)

func TestDestinationAllowed(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1":              false,
		"10.1.2.3":               false,
		"172.17.0.2":             false,
		"192.168.1.1":            false,
		"169.254.169.254":        false,
		"100.64.0.1":             false,
		"100.127.255.254":        false,
		"0.0.0.0":                false,
		"0.1.2.3":                false,
		"224.0.0.1":              false,
		"::1":                    false,
		"::":                     false,
		"fd00::1":                false,
		"fe80::1":                false,
		"ff02::1":                false,
		"::ffff:127.0.0.1":       false,
		"::ffff:169.254.169.254": false,
		"93.184.216.34":          true,
		"100.128.0.1":            true,
		"2606:4700::1111":        true,
	} {
		if got := destinationAllowed(netip.MustParseAddr(addr)); got != want {
			t.Errorf("destinationAllowed(%s) = %v, want %v", addr, got, want)
		}
	}
}
