package ci

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLocalAddr(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1:5000":         true,
		"[::1]:5000":             true,
		"192.168.1.20:5000":      true,
		"10.0.0.5:1":             true,
		"172.16.4.4:1":           true,
		"[fd00::1]:1":            true,
		"[fe80::1]:1":            true,
		"169.254.10.1:1":         true,
		"[::ffff:192.168.1.2]:1": true,
		"8.8.8.8:5000":           false,
		"172.32.0.1:1":           false,
		"[2001:db8::1]:1":        false,
		"garbage":                false,
	} {
		if got := localAddr(addr); got != want {
			t.Errorf("localAddr(%q) = %v, want %v", addr, got, want)
		}
	}
}

func TestLanCancel(t *testing.T) {
	cases := []struct {
		name     string
		lan      bool
		remote   string
		origin   string
		token    string
		wantCode int
	}{
		{"off: no token refused", false, "192.168.1.5:1", "", "", http.StatusUnauthorized},
		{"on: LAN, no origin", true, "192.168.1.5:1", "", "", http.StatusOK},
		{"on: LAN, LAN origin", true, "192.168.1.5:1", "http://192.168.1.9:9516", "", http.StatusOK},
		{"on: LAN, .local origin", true, "192.168.1.5:1", "http://buildbox.local:9516", "", http.StatusOK},
		{"on: LAN, same host origin", true, "192.168.1.5:1", "http://example.lan:9516", "", http.StatusOK},
		{"on: LAN, internet origin refused", true, "192.168.1.5:1", "https://evil.example.com", "", http.StatusUnauthorized},
		{"on: internet address refused", true, "8.8.8.8:1", "", "", http.StatusUnauthorized},
		{"on: wrong token refused, not waved through", true, "192.168.1.5:1", "", "nope", http.StatusUnauthorized},
		{"off: token from anywhere", false, "8.8.8.8:1", "https://evil.example.com", "tok", http.StatusOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newArtifactService(t)
			s.Config.LanCancel = c.lan
			h := &Handler{Service: s, Token: "tok"}
			called := false
			fn := h.authOrLAN(func(w http.ResponseWriter, r *http.Request) { called = true })
			r := httptest.NewRequest(http.MethodPost, "http://example.lan:9516/api/ci/jobs/x/cancel", nil)
			r.RemoteAddr = c.remote
			if c.origin != "" {
				r.Header.Set("Origin", c.origin)
			}
			if c.token != "" {
				r.Header.Set("Authorization", "Bearer "+c.token)
			}
			w := httptest.NewRecorder()
			fn(w, r)
			code := w.Code
			if called {
				code = http.StatusOK
			}
			if code != c.wantCode {
				t.Fatalf("got %d (called=%v), want %d", code, called, c.wantCode)
			}
		})
	}
}

func TestStatusReportsLanCancel(t *testing.T) {
	s := newArtifactService(t)
	s.Config.LanCancel = true
	h := &Handler{Service: s}
	w := httptest.NewRecorder()
	h.status(w, httptest.NewRequest(http.MethodGet, "/api/ci/status", nil))
	if !contains(w.Body.String(), `"lan_cancel":true`) {
		t.Fatalf("status body %s", w.Body.String())
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
