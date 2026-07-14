package mikrotik

import "time"

const dedupTTL = 30 * time.Second

func (dw *deviceWriter) dedupKey(addr, list string) string {
	return dw.cfg.Address + "|" + list + "|" + addr
}

func (dw *deviceWriter) isDeduped(addr, list string) bool {
	k := dw.dedupKey(addr, list)
	v, ok := dw.dedup.Load(k)
	if !ok {
		return false
	}
	deadline, ok := v.(time.Time)
	if !ok || time.Now().After(deadline) {
		dw.dedup.Delete(k) // lazy cleanup of expired key
		return false
	}
	return true
}

func (dw *deviceWriter) markDeduped(addr, list string) {
	dw.dedup.Store(dw.dedupKey(addr, list), time.Now().Add(dedupTTL))
}
