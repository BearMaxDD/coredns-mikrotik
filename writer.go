package mikrotik

import (
	"context"
	"log"
	"time"

	ros "github.com/go-routeros/routeros/v3"
)

// processItem handles a single address-list write request.
func (dw *deviceWriter) processItem(ctx context.Context, item writeItem) {
	// 1. 失败退避检查
	if !dw.nextAllowed.IsZero() && time.Now().Before(dw.nextAllowed) {
		writesCount.WithLabelValues(dw.cfg.Address, item.list, "backoff").Inc()
		return
	}

	// 2. Write-cache 检查（用 mask 后的 target）
	target := applyMask(item.address, item.mask)
	ck := cacheKey(dw.cfg.Address, item.list, target)
	if dw.wcache != nil && dw.wcache.Has(ck) {
		if dw.cfg.RefreshOnHit {
			if err := dw.ensureConnected(ctx); err != nil {
				log.Printf("mikrotik: dial %s: %v", dw.cfg.Address, err)
				writesCount.WithLabelValues(dw.cfg.Address, item.list, "error").Inc()
				dw.backoffFailure()
				return
			}
			comment := item.domain
			if comment == "" {
				comment = dw.cfg.Comment
			}
			if err := writeToRouterOS(ctx, dw.client, item.address, item.list, dw.cfg.Timeout, comment, item.mask); err != nil {
				log.Printf("mikrotik: refresh %s/%s: %v", dw.cfg.Address, item.list, err)
				dw.closeClient()
				writesCount.WithLabelValues(dw.cfg.Address, item.list, "error").Inc()
				dw.backoffFailure()
				return
			}
			dw.wcache.Set(ck)
			dw.backoffSuccess()
			writesCount.WithLabelValues(dw.cfg.Address, item.list, "written").Inc()
			return
		}
		writesCount.WithLabelValues(dw.cfg.Address, item.list, "cache_hit").Inc()
		return
	}

	// 3. 连接
	if err := dw.ensureConnected(ctx); err != nil {
		log.Printf("mikrotik: dial %s: %v", dw.cfg.Address, err)
		writesCount.WithLabelValues(dw.cfg.Address, item.list, "error").Inc()
		dw.backoffFailure()
		return
	}

	// 4. 写入 RouterOS（comment = 域名，去尾部 dot）
	comment := item.domain
	if comment == "" {
		comment = dw.cfg.Comment // fallback
	}
	err := writeToRouterOS(ctx, dw.client, item.address, item.list, dw.cfg.Timeout, comment, item.mask)
	if err != nil {
		log.Printf("mikrotik: write %s/%s: %v", dw.cfg.Address, item.list, err)
		dw.closeClient()
		writesCount.WithLabelValues(dw.cfg.Address, item.list, "error").Inc()
		dw.backoffFailure()
		return
	}

	// 5. 成功：写入 cache + 重置退避
	if dw.wcache != nil {
		dw.wcache.Set(ck)
	}
	dw.backoffSuccess()
	writesCount.WithLabelValues(dw.cfg.Address, item.list, "written").Inc()
}

func (dw *deviceWriter) backoffFailure() {
	if dw.backoff == 0 {
		dw.backoff = time.Second
	} else {
		dw.backoff *= 2
	}
	if dw.backoff > time.Minute {
		dw.backoff = time.Minute
	}
	dw.nextAllowed = time.Now().Add(dw.backoff)
}

func (dw *deviceWriter) backoffSuccess() {
	dw.backoff = 0
	dw.nextAllowed = time.Time{}
}

func (dw *deviceWriter) ensureConnected(ctx context.Context) error {
	if dw.client != nil {
		return nil
	}
	c, err := ros.DialContext(ctx, dw.cfg.Address, dw.cfg.Username, dw.cfg.Password)
	if err != nil {
		return err
	}
	dw.client = c
	return nil
}

func (dw *deviceWriter) closeClient() {
	if dw.client != nil {
		dw.client.Close()
		dw.client = nil
	}
}

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
