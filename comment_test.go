package mikrotik

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
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
	dw.processItem(context.Background(), writeItem{
		address: "104.20.26.136", list: "allowed", mask: 32, domain: "openai.com",
	})
	fc.mu.Lock()
	history := fc.history
	fc.mu.Unlock()
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
	dw.wcache.Set(cacheKey("10.0.0.1:8728", "allowed", "104.20.26.136/32"))
	dw.processItem(context.Background(), writeItem{
		address: "104.20.26.136", list: "allowed", mask: 32, domain: "chatgpt.com",
	})
	fc.mu.Lock()
	history := fc.history
	fc.mu.Unlock()
	if len(history) != 0 {
		t.Errorf("expected 0 commands on cache hit, got %d: %v", len(history), history)
	}
}

func TestServeDNSDomainNormalized(t *testing.T) {
	fc := &fakeClient{}
	domainPath := writeDomainFile(t, "openai.com\n")
	dl, err := NewDomainList(domainPath, 0)
	if err != nil {
		t.Fatal(err)
	}
	m := &Mikrotik{
		routes: []RouteRule{{
			Matcher:      dl,
			AddressList4: "test",
		}},
		writers: []*deviceWriter{{
			cfg:    DeviceConfig{Address: "10.0.0.1:8728"},
			queue:  make(chan writeItem, 10),
			wcache: newWriteCache(time.Hour),
			client: fc,
		}},
		exchange: func(ctx context.Context, r *dns.Msg) (*dns.Msg, error) {
			resp := new(dns.Msg)
			resp.SetReply(r)
			resp.Answer = append(resp.Answer, &dns.A{
				Hdr: dns.RR_Header{Name: "OpenAI.COM.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
				A:   net.ParseIP("10.0.0.1").To4(),
			})
			return resp, nil
		},
	}
	r := new(dns.Msg)
	r.SetQuestion("OpenAI.COM.", dns.TypeA)
	m.ServeDNS(context.Background(), &testResponseWriter{}, r)
	select {
	case item := <-m.writers[0].queue:
		if item.domain != "openai.com" {
			t.Fatalf("domain = %q, want %q", item.domain, "openai.com")
		}
	default:
		t.Fatal("expected item in queue")
	}
}

func TestCommentChangesAfterCacheExpiry(t *testing.T) {
	fc := &fakeClient{}
	// 首次 print+add 各用一个 reply，第二次 print 用有 entry 的 reply，set 再用一个
	fc.setReplies([]string{"", "", ".id=*1\naddress=10.0.0.5\nlist=allowed\ncomment=a.example.com", ""})
	dw := &deviceWriter{
		cfg:    DeviceConfig{Address: "10.0.0.1:8728"},
		queue:  make(chan writeItem, 10),
		wcache: &writeCache{entries: make(map[string]time.Time), ttl: time.Hour},
		client: fc,
	}
	// 首次写入：域名 A（mask=0 → target="10.0.0.5"）
	dw.processItem(context.Background(), writeItem{
		address: "10.0.0.5", list: "allowed", mask: 0, domain: "a.example.com",
	})
	fc.mu.Lock()
	initialCmds := len(fc.history)
	fc.mu.Unlock()

	// 模拟 cache 过期
	dw.wcache = &writeCache{entries: make(map[string]time.Time), ttl: time.Hour}

	// 过期后：域名 B
	dw.processItem(context.Background(), writeItem{
		address: "10.0.0.5", list: "allowed", mask: 0, domain: "b.example.com",
	})
	fc.mu.Lock()
	cmds := fc.history
	fc.mu.Unlock()

	if len(cmds) < initialCmds+2 {
		t.Fatalf("expected 2 new commands after expiry, had %d then %d", initialCmds, len(cmds))
	}
	// 倒数第二条应是 print，最后一条应是 set
	if cmds[len(cmds)-1][0] != "/ip/firewall/address-list/set" {
		t.Fatalf("last command expected /set, got %v", cmds[len(cmds)-1])
	}
	hasComment := false
	for _, arg := range cmds[len(cmds)-1] {
		if arg == "=comment=b.example.com" {
			hasComment = true
		}
	}
	if !hasComment {
		t.Errorf("expected =comment=b.example.com in set, got %v", cmds[len(cmds)-1])
	}
}
