package mikrotik

import (
	"context"
	"log"

	ros "github.com/go-routeros/routeros/v3"
)

// processItem handles a single address-list write request: dedup check,
// ensure connection, write to RouterOS, mark dedup, and update metrics.
func (dw *deviceWriter) processItem(ctx context.Context, item writeItem) {
	if dw.isDeduped(item.address, item.list) {
		writesCount.WithLabelValues(dw.cfg.Address, item.list, "dedup_hit").Inc()
		return
	}
	if err := dw.ensureConnected(ctx); err != nil {
		log.Printf("mikrotik: dial %s: %v", dw.cfg.Address, err)
		writesCount.WithLabelValues(dw.cfg.Address, item.list, "error").Inc()
		return
	}
	err := writeToRouterOS(ctx, dw.client, item.address, item.list, dw.cfg.Timeout, dw.cfg.Comment, 0)
	if err != nil {
		log.Printf("mikrotik: write %s/%s: %v", dw.cfg.Address, item.list, err)
		dw.closeClient()
		writesCount.WithLabelValues(dw.cfg.Address, item.list, "error").Inc()
		return
	}
	dw.markDeduped(item.address, item.list)
	writesCount.WithLabelValues(dw.cfg.Address, item.list, "written").Inc()
}

// ensureConnected lazily dials a RouterOS connection if one is not already
// established. Returns nil on success or if a client already exists.
func (dw *deviceWriter) ensureConnected(ctx context.Context) error {
	if dw.client != nil {
		return nil
	}
	client, err := ros.DialContext(ctx, dw.cfg.Address, dw.cfg.Username, dw.cfg.Password)
	if err != nil {
		return err
	}
	dw.client = client
	return nil
}

// closeClient closes the RouterOS client if non-nil and sets it to nil.
func (dw *deviceWriter) closeClient() {
	if dw.client != nil {
		dw.client.Close()
		dw.client = nil
	}
}

// drain consumes all pending queue items until the context expires or the
// queue is empty.
func (dw *deviceWriter) drain(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-dw.queue:
		default:
			return
		}
	}
}
