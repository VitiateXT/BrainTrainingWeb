package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

// Which client addresses count as "on the LAN".
func TestIsLAN(t *testing.T) {
	for addr, want := range map[string]bool{
		"192.168.188.7:1": true, "10.0.0.1:1": true, "127.0.0.1:1": true, "[::1]:1": true,
		"[::ffff:192.168.1.1]:1": true, "[fd00::1]:1": true, "[fe80::1]:1": true,
		"8.8.8.8:1": false, "[2001:db8::1]:1": false, "garbage": false,
	} {
		if got := isLAN(addr); got != want {
			t.Errorf("isLAN(%q) = %v, want %v", addr, got, want)
		}
	}
}

// do sends one request from a LAN address and returns the answer. mod, if
// given, can change the request first, for example to add the session cookie.
func do(h http.Handler, method, target, body string, mod func(*http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.RemoteAddr = "192.168.1.5:1234"
	if method == "POST" {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if mod != nil {
		mod(r)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// Session tokens: only an untouched, unexpired one is valid.
func TestSession(t *testing.T) {
	s, _ := newServer("pw")
	future := time.Now().Add(time.Hour).Unix()
	good := s.sign(future)
	tampered := strings.Replace(good, strconv.FormatInt(future, 10), strconv.FormatInt(future+1, 10), 1)
	for name, tc := range map[string]struct {
		tok  string
		want bool
	}{
		"good": {good, true}, "expired": {s.sign(time.Now().Add(-time.Hour).Unix()), false},
		"tampered": {tampered, false}, "garbage": {"x", false}, "empty": {"", false},
	} {
		if got := s.valid(tc.tok); got != tc.want {
			t.Errorf("%s: valid = %v, want %v", name, got, tc.want)
		}
	}
}

// The whole server through its HTTP routes, in the order a browser uses them:
// login page, refused logins, login, games page, logout.
func TestFlow(t *testing.T) {
	s, _ := newServer("pw")
	s.delay = 0
	h := s.routes()
	isLogin := func(w *httptest.ResponseRecorder) bool { return strings.Contains(w.Body.String(), `action="/login"`) }

	w := do(h, "GET", "/", "", nil)
	if w.Code != 200 || !isLogin(w) || w.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("page without cookie = %d %q, want the login page, not cached", w.Code, w.Header().Get("Cache-Control"))
	}
	if c := do(h, "GET", "/", "", func(r *http.Request) { r.RemoteAddr = "8.8.8.8:1" }).Code; c != 403 {
		t.Errorf("public IP = %d, want 403", c)
	}
	if c := do(h, "GET", "/index.html", "", nil).Code; c != 404 {
		t.Errorf("GET /index.html = %d, want 404", c)
	}

	w = do(h, "POST", "/login", "pw=wrong", nil)
	if w.Code != 303 || w.Header().Get("Location") != "/?wrong" || len(w.Result().Cookies()) != 0 {
		t.Errorf("wrong password = %d to %q with %d cookies, want 303 to /?wrong, none", w.Code, w.Header().Get("Location"), len(w.Result().Cookies()))
	}
	// POSTs marked by the browser as coming from another website or another
	// app on this PC are refused, even with the right password. The app's own
	// page and tools without these headers pass.
	for _, tc := range []struct {
		hdr, val string
		want     int
	}{
		{"Sec-Fetch-Site", "cross-site", 403}, {"Sec-Fetch-Site", "same-site", 403}, {"Origin", "https://evil.example", 403},
		{"Origin", "null", 403}, {"Sec-Fetch-Site", "same-origin", 303}, {"Origin", "https://example.com", 303}, {"", "", 303},
	} {
		if c := do(h, "POST", "/login", "pw=pw", func(r *http.Request) { r.Header.Set(tc.hdr, tc.val) }).Code; c != tc.want {
			t.Errorf("login with %s %q = %d, want %d", tc.hdr, tc.val, c, tc.want)
		}
	}
	if c := do(h, "POST", "/login", "", func(r *http.Request) { r.Body = io.NopCloser(iotest.ErrReader(io.ErrUnexpectedEOF)) }).Code; c != 400 {
		t.Errorf("login with a broken body = %d, want 400", c)
	}
	if c := do(h, "POST", "/login", "pw="+strings.Repeat("x", 2000), nil).Code; c != 400 {
		t.Errorf("login with an oversized body = %d, want 400", c)
	}

	login := do(h, "POST", "/login", "pw=pw", nil)
	cs := login.Result().Cookies()
	if login.Code != 303 || login.Header().Get("Location") != "/" || len(cs) != 1 {
		t.Fatalf("login = %d to %q, cookies = %d", login.Code, login.Header().Get("Location"), len(cs))
	}
	if c := cs[0]; c.Name != "bt" || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode {
		t.Errorf("cookie wrong: %+v", c)
	}
	w = do(h, "GET", "/", "", func(r *http.Request) { r.AddCookie(cs[0]) })
	if w.Code != 200 || isLogin(w) || !strings.Contains(w.Body.String(), "Calculator Mode") {
		t.Errorf("page with cookie = %d, want the games", w.Code)
	}
	forged := &http.Cookie{Name: "bt", Value: cs[0].Value + "0"}
	if w := do(h, "GET", "/", "", func(r *http.Request) { r.AddCookie(forged) }); !isLogin(w) {
		t.Error("page with a forged cookie did not show the login page")
	}

	out := do(h, "POST", "/logout", "", func(r *http.Request) { r.AddCookie(cs[0]) })
	oc := out.Result().Cookies()
	if out.Code != 303 || len(oc) != 1 || oc[0].Name != "bt" || oc[0].Value != "" || oc[0].MaxAge >= 0 {
		t.Errorf("logout = %d, cookies %+v, want one empty, expired bt cookie", out.Code, oc)
	}
	if c := do(h, "POST", "/logout", "", func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }).Code; c != 403 {
		t.Errorf("cross-site logout = %d, want 403", c)
	}
}
