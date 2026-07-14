package mikrotik

import "time"

const dedupTTL = 30 * time.Second

func (dw *deviceWriter) dedupKey(addr, list string) string {
	return dw.cfg.Address + "|" + list + "|" + addr
}

func (dw *deviceWriter) isDeduped(addr, list string) bool {
	v, ok := dw.dedup.Load(dw.dedupKey(addr, list))
	if !ok {
		return false
	}
	deadline, ok := v.(time.Time)
	return ok && time.Now().Before(deadline)
}

func (dw *deviceWriter) markDeduped(addr, list string) {
	dw.dedup.Store(dw.dedupKey(addr, list), time.Now().Add(dedupTTL))
}
