package netd

import "strconv"

// Pool numbers one of the host's independent sprite networks, each with its own
// bridge, taps, nftables table and sandpit-netd. One sandpitd owns a pool at a time, so a
// second networked sandpitd (a test stack beside production) needs a second pool.
// Pool 0 has the names the first one always had; scripts/setup-host.sh derives the
// same names from WISP_POOL, so keep the two in step.
type Pool int

// suffix is what follows "wisp" or "msbr" in pool-specific names: nothing for pool 0.
func (p Pool) suffix() string {
	if p == 0 {
		return ""
	}
	return strconv.Itoa(int(p))
}

// Bridge is the pool's bridge device.
func (p Pool) Bridge() string { return "msbr" + strconv.Itoa(int(p)) }

// TapPrefix starts every tap device name in the pool, and no other pool's:
// "ms1tap" cannot be confused with "mstap".
func (p Pool) TapPrefix() string {
	if p == 0 {
		return "mstap"
	}
	return "ms" + strconv.Itoa(int(p)) + "tap"
}

// Table is the pool's nftables table, in the inet family.
func (p Pool) Table() string { return "wisp" + p.suffix() }

// Set is the nft set sandpit-netd edits: sprites in it are diverted to sandpitd's
// policy listeners.
func (p Pool) Set() string { return "inet " + p.Table() + " restricted4" }

// Socket is where the pool's sandpit-netd listens. Each pool's helper has its own
// RuntimeDirectory, because systemd deletes that directory when the unit stops.
func (p Pool) Socket() string { return "/run/wisp" + p.suffix() + "/netd.sock" }
