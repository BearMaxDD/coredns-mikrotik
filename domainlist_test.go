package mikrotik

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeDomainFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "domains.txt")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDomainListMatch(t *testing.T) {
	content := "example.com\nblocked.test\n"
	path := writeDomainFile(t, content)

	dl, err := NewDomainList(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer dl.Close()

	t.Run("match existing domain", func(t *testing.T) {
		if !dl.Match("example.com") {
			t.Error("expected match for example.com")
		}
	})

	t.Run("match existing domain with trailing dot", func(t *testing.T) {
		if !dl.Match("example.com.") {
			t.Error("expected match for example.com.")
		}
	})

	t.Run("match blocked.test", func(t *testing.T) {
		if !dl.Match("blocked.test") {
			t.Error("expected match for blocked.test")
		}
	})

	t.Run("no match for absent domain", func(t *testing.T) {
		if dl.Match("missing.test") {
			t.Error("expected no match for missing.test")
		}
	})
}

func TestDomainListTrailingDotAutoComplete(t *testing.T) {
	// Domains without trailing dot in file should still match with or without dot
	content := "example.com\n"
	path := writeDomainFile(t, content)

	dl, err := NewDomainList(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer dl.Close()

	// Queries without trailing dot
	if !dl.Match("example.com") {
		t.Error("expected match for example.com (query without dot)")
	}

	// Queries with trailing dot
	if !dl.Match("example.com.") {
		t.Error("expected match for example.com. (query with dot)")
	}
}

func TestDomainListCaseInsensitive(t *testing.T) {
	content := "Example.COM\nUPPER.test\n"
	path := writeDomainFile(t, content)

	dl, err := NewDomainList(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer dl.Close()

	cases := []struct {
		query string
		want  bool
	}{
		{"example.com", true},
		{"EXAMPLE.COM", true},
		{"Example.Com", true},
		{"upper.test", true},
		{"Upper.Test", true},
		{"UPPER.TEST", true},
		{"missing.test", false},
	}

	for _, tc := range cases {
		got := dl.Match(tc.query)
		if got != tc.want {
			t.Errorf("Match(%q) = %v, want %v", tc.query, got, tc.want)
		}
	}
}

func TestDomainListSkipCommentsAndEmptyLines(t *testing.T) {
	content := "# this is a comment\n\n# another comment\nexample.com\n   \nblocked.test\n# trailing comment\n"
	path := writeDomainFile(t, content)

	dl, err := NewDomainList(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer dl.Close()

	if !dl.Match("example.com") {
		t.Error("expected match for example.com (after skipping comments and blanks)")
	}
	if !dl.Match("blocked.test") {
		t.Error("expected match for blocked.test")
	}
	if dl.Match("this is a comment") {
		t.Error("expected no match for a comment line")
	}
}

func TestDomainListNonexistentFile(t *testing.T) {
	_, err := NewDomainList("/nonexistent/path/domains.txt", 0)
	if err == nil {
		t.Error("expected error for nonexistent file path")
	}
}

func TestDomainListReloadInterval(t *testing.T) {
	content := "first.com\n"
	path := writeDomainFile(t, content)

	dl, err := NewDomainList(path, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer dl.Close()

	if !dl.Match("first.com") {
		t.Error("expected match for first.com before reload")
	}

	// Rewrite the file with new content
	content2 := "second.com\n"
	if err := os.WriteFile(path, []byte(content2), 0644); err != nil {
		t.Fatal(err)
	}

	// Wait for reload to pick up changes
	time.Sleep(200 * time.Millisecond)

	if !dl.Match("second.com") {
		t.Error("expected match for second.com after reload")
	}
	// Old domain should be gone (full replacement)
	if dl.Match("first.com") {
		t.Error("expected no match for first.com after file replaced")
	}
}

func TestDomainListSubdomainMatch(t *testing.T) {
	content := "example.com\nblocked.test\n"
	path := writeDomainFile(t, content)

	dl, err := NewDomainList(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer dl.Close()

	t.Run("subdomain matches parent", func(t *testing.T) {
		if !dl.Match("sub.example.com") {
			t.Error("expected match for sub.example.com")
		}
	})

	t.Run("deep subdomain matches", func(t *testing.T) {
		if !dl.Match("a.b.c.sub.example.com") {
			t.Error("expected match for deep subdomain")
		}
	})
}

func TestDomainListLabelBoundary(t *testing.T) {
	content := "example.com\n"
	path := writeDomainFile(t, content)

	dl, err := NewDomainList(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer dl.Close()

	// "badexample.com" should NOT match "example.com"
	if dl.Match("badexample.com") {
		t.Error("expected no match for badexample.com (label boundary)")
	}
	if dl.Match("notexample.com") {
		t.Error("expected no match for notexample.com (label boundary)")
	}
}
