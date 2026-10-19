package handlers

import (
	"sync"
	"time"
)

type probeReceipt struct {
	at     time.Time
	probes map[int64]time.Time
}

var probeReceipts = struct {
	sync.Mutex
	devices map[string]*probeReceipt
}{devices: map[string]*probeReceipt{}}

func (h *Handlers) acceptProbeResult(device string, probe int64, now time.Time) bool {
	probeReceipts.Lock()
	defer probeReceipts.Unlock()
	for id, r := range probeReceipts.devices {
		if now.Sub(r.at) > 10*time.Minute {
			delete(probeReceipts.devices, id)
		}
	}
	r := probeReceipts.devices[device]
	if r == nil {
		if len(probeReceipts.devices) >= 4096 {
			return false
		}
		r = &probeReceipt{probes: map[int64]time.Time{}}
		probeReceipts.devices[device] = r
	}
	r.at = now
	for id, at := range r.probes {
		if now.Sub(at) > time.Minute {
			delete(r.probes, id)
		}
	}
	if last := r.probes[probe]; !last.IsZero() && now.Sub(last) < 30*time.Second {
		return false
	}
	if len(r.probes) >= 64 {
		return false
	}
	r.probes[probe] = now
	return true
}
