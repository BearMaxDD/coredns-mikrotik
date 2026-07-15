package mikrotik

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestCommentNormalized(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"OpenAI.COM.", "openai.com"},
		{"openai.com.", "openai.com"},
		{"example.org.", "example.org"},
		{"OpenAI.COM", "openai.com"},
	}
	for _, tc := range tests {
		got := strings.ToLower(strings.TrimSuffix(tc.input, "."))
		if got != tc.want {
			t.Errorf("normalize(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestWriteCommentFromItem(t *testing.T) {
	fc := &fakeClient{}
	dw := &deviceWriter{
		cfg:    DeviceConfig{Address: "10.0.0.1:8728", Comment: "fallback"},
		queue:  make(chan writeItem, 10),
		wcache: newWriteCache(time.Hour),
		client: fc,
	}

	// 写入，domain = "openai.com"
	dw.processItem(context.Background(), writeItem{
		address: "104.20.26.136",
		list:    "allowed",
		mask:    32,
		domain:  "openai.com",
	})

	fc.mu.Lock()
	history := fc.history
	fc.mu.Unlock()

	// add 命令中应包含 =comment=openai.com
	found := false
	for _, cmd := range history {
		for _, arg := range cmd {
			if arg == "=comment=openai.com" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected comment=openai.com in commands, got %v", history)
	}
}

func TestCommentCacheHitDoesNotUpdate(t *testing.T) {
	fc := &fakeClient{}
	dw := &deviceWriter{
		cfg:    DeviceConfig{Address: "10.0.0.1:8728"},
		queue:  make(chan writeItem, 10),
		wcache: newWriteCache(time.Hour),
		client: fc,
	}

	// processItem 内部计算 target = applyMask("104.20.26.136", 32) = "104.20.26.136/32"
	dw.wcache.Set(cacheKey("10.0.0.1:8728", "allowed", "104.20.26.136/32"))

	// 域名 B 被缓存挡掉
	dw.processItem(context.Background(), writeItem{
		address: "104.20.26.136",
		list:    "allowed",
		mask:    32,
		domain:  "chatgpt.com",
	})

	fc.mu.Lock()
	history := fc.history
	fc.mu.Unlock()

	if len(history) != 0 {
		t.Errorf("expected 0 commands on cache hit, got %d: %v", len(history), history)
	}
}
