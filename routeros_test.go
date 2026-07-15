package mikrotik

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	ros "github.com/go-routeros/routeros/v3"
	"github.com/go-routeros/routeros/v3/proto"
)

// fakeClient implements rosClient for testing.
type fakeClient struct {
	mu       sync.Mutex
	history  [][]string
	replies  []string
	replyIdx int
}

func (f *fakeClient) RunArgsContext(_ context.Context, args []string) (*ros.Reply, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Copy the slice so the caller's mutations don't affect history.
	entry := make([]string, len(args))
	copy(entry, args)
	f.history = append(f.history, entry)

	if f.replyIdx >= len(f.replies) {
		return &ros.Reply{}, nil
	}
	replyStr := f.replies[f.replyIdx]
	f.replyIdx++

	if replyStr == "" {
		return &ros.Reply{}, nil
	}

	m := make(map[string]string)
	for _, line := range strings.Split(replyStr, "\n") {
		if idx := strings.IndexByte(line, '='); idx >= 0 {
			m[line[:idx]] = line[idx+1:]
		}
	}
	return &ros.Reply{
		Re: []*proto.Sentence{{Map: m}},
	}, nil
}

func (f *fakeClient) Close() error { return nil }

func (f *fakeClient) setReplies(r []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replies = r
	f.replyIdx = 0
	f.history = nil
}

func TestTimeoutToRouterOSString(t *testing.T) {
	tests := []struct {
		name     string
		input    time.Duration
		expected string
	}{
		{"24h", 24 * time.Hour, "24:00:00"},
		{"25h30m", 25*time.Hour + 30*time.Minute, "25:30:00"},
		{"1h30m", 1*time.Hour + 30*time.Minute, "01:30:00"},
		{"90s", 90 * time.Second, "00:01:30"},
		{"zero", 0, "0s"},
		{"negative", -1 * time.Hour, "0s"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := timeoutToRouterOSString(tc.input)
			if got != tc.expected {
				t.Errorf("timeoutToRouterOSString(%v) = %q; want %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestCmdPathForAddr(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"ipv4", "10.0.0.5", "/ip/firewall/address-list"},
		{"ipv6", "2001:db8::1", "/ipv6/firewall/address-list"},
		{"invalid", "not-an-ip", "/ipv6/firewall/address-list"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := cmdPathForAddr(tc.input)
			if got != tc.expected {
				t.Errorf("cmdPathForAddr(%q) = %q; want %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestApplyMask(t *testing.T) {
	tests := []struct {
		name string
		addr string
		mask int
		want string
	}{
		{"ipv4_24", "10.0.0.5", 24, "10.0.0.0/24"},
		{"ipv4_32", "10.0.0.5", 32, "10.0.0.5/32"},
		{"ipv4_mask0", "10.0.0.5", 0, "10.0.0.5"},
		{"ipv4_negative", "10.0.0.5", -1, "10.0.0.5"},
		{"ipv6_64", "2001:db8::1", 64, "2001:db8::/64"},
		{"ipv6_128", "2001:db8::1", 128, "2001:db8::1/128"},
		{"ipv6_mask0", "2001:db8::1", 0, "2001:db8::1"},
		{"ipv4_mask_overflow", "10.0.0.5", 48, "10.0.0.5/32"},
		{"ipv6_mask_overflow", "2001:db8::1", 192, "2001:db8::1/128"},
		{"invalid_addr", "not-an-ip", 24, "not-an-ip"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := applyMask(tc.addr, tc.mask)
			if got != tc.want {
				t.Errorf("applyMask(%q, %d) = %q; want %q", tc.addr, tc.mask, got, tc.want)
			}
		})
	}
}

func TestWriteToRouterOS_Add(t *testing.T) {
	fc := &fakeClient{}
	fc.setReplies([]string{""}) // print returns no entries

	err := writeToRouterOS(context.Background(), fc, "10.0.0.5", "allowed", 24*time.Hour, "", 24)
	if err != nil {
		t.Fatalf("writeToRouterOS returned error: %v", err)
	}

	fc.mu.Lock()
	defer fc.mu.Unlock()

	if len(fc.history) != 2 {
		t.Fatalf("expected 2 client calls, got %d", len(fc.history))
	}

	// First call: print
	wantPrint := []string{"/ip/firewall/address-list/print", "?address=10.0.0.0/24", "?list=allowed"}
	if !slicesEqual(fc.history[0], wantPrint) {
		t.Errorf("print call args = %v; want %v", fc.history[0], wantPrint)
	}

	// Second call: add — note the masked address
	wantAdd := []string{"/ip/firewall/address-list/add", "=address=10.0.0.0/24", "=list=allowed", "=timeout=24:00:00"}
	if !slicesEqual(fc.history[1], wantAdd) {
		t.Errorf("add call args = %v; want %v", fc.history[1], wantAdd)
	}
}

func TestWriteToRouterOS_AddNoMask(t *testing.T) {
	fc := &fakeClient{}
	fc.setReplies([]string{""}) // print returns no entries

	err := writeToRouterOS(context.Background(), fc, "10.0.0.5", "allowed", 24*time.Hour, "", 0)
	if err != nil {
		t.Fatalf("writeToRouterOS returned error: %v", err)
	}

	fc.mu.Lock()
	defer fc.mu.Unlock()

	if len(fc.history) != 2 {
		t.Fatalf("expected 2 client calls, got %d", len(fc.history))
	}

	// Second call: add — no mask applied, original address
	wantAdd := []string{"/ip/firewall/address-list/add", "=address=10.0.0.5", "=list=allowed", "=timeout=24:00:00"}
	if !slicesEqual(fc.history[1], wantAdd) {
		t.Errorf("add call args = %v; want %v", fc.history[1], wantAdd)
	}
}

func TestWriteToRouterOS_UpdateExisting(t *testing.T) {
	fc := &fakeClient{}
	fc.setReplies([]string{".id=*1"}) // print returns one existing entry

	err := writeToRouterOS(context.Background(), fc, "10.0.0.5", "allowed", 24*time.Hour, "", 24)
	if err != nil {
		t.Fatalf("writeToRouterOS returned error: %v", err)
	}

	fc.mu.Lock()
	defer fc.mu.Unlock()

	if len(fc.history) != 2 {
		t.Fatalf("expected 2 client calls, got %d", len(fc.history))
	}
	wantPrint := []string{"/ip/firewall/address-list/print", "?address=10.0.0.0/24", "?list=allowed"}
	// First call: print
	if !slicesEqual(fc.history[0], wantPrint) {
		t.Errorf("print call args = %v; want %v", fc.history[0], wantPrint)
	}

	// Second call: set — address from RouterOS print output, not affected by mask
	wantSet := []string{"/ip/firewall/address-list/set", "=.id=*1", "=timeout=24:00:00"}
	if !slicesEqual(fc.history[1], wantSet) {
		t.Errorf("set call args = %v; want %v", fc.history[1], wantSet)
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
