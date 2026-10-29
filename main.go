// BrainTrainingWeb serves the BrainTraining mini-games (calculator, remembering
// and colour mode) as one web page over HTTPS, behind a single password. The
// games run entirely in the browser; the server only checks the password and
// hands out the page. It uses only the Go standard library.
//
// Routes:
//
//	GET  /        the games (index.html) when logged in, else the login page
//	POST /login   checks the password and sets the session cookie
//	POST /logout  deletes the session cookie
//
// Configuration is read from environment variables:
//
//	BT_PASSWORD  the login password (required)
//	BT_ADDR      listen address      (default :8444)
//	BT_CERT      TLS certificate     (default ~/.config/braintraining/cert.pem)
//	BT_KEY       TLS private key     (default ~/.config/braintraining/key.pem)
package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	_ "embed"
	"encoding/hex"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// indexHTML is the games page and loginHTML the password form. Both are
// compiled into the binary so that deploying means copying a single file.
//
//go:embed index.html
var indexHTML []byte

//go:embed login.html
var loginHTML []byte

// sessionTTL is how long a login stays valid.
const sessionTTL = 30 * 24 * time.Hour

// cookieName is the session cookie. Browsers send cookies to every port on
// the same host, so it must differ from the other web apps on this PC
// (HomeStream uses "hs").
const cookieName = "bt"

// server holds the state shared by all request handlers.
type server struct {
	// pwHash is the SHA-256 of the password. Login compares hashes (always 32
	// bytes) in constant time instead of comparing the passwords themselves.
	pwHash [32]byte
	// secret signs the session cookies. It is random on every start, so a
	// restart logs everyone out.
	secret []byte
	// delay is the pause after a wrong password.
	delay time.Duration
	// loginMu serialises login attempts so that at most one guess per delay
	// is possible, however many connections an attacker opens.
	loginMu sync.Mutex
}

// newServer prepares a server for the given password.
func newServer(password string) (*server, error) {
	s := &server{pwHash: sha256.Sum256([]byte(password)), secret: make([]byte, 32), delay: time.Second}
	_, err := rand.Read(s.secret)
	return s, err
}

// routes builds the request handler, wrapped in guard, which enforces the
// LAN-only and same-origin rules.
func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.page)
	mux.HandleFunc("POST /login", s.login)
	mux.HandleFunc("POST /logout", logout)
	return guard(mux)
}

// isLAN reports whether a "host:port" client address is on the local network:
// loopback, private (10.x, 172.16-31.x, 192.168.x, fc00::/7) or link-local.
// Anything else, such as a public address, is refused.
func isLAN(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		return false
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	a = a.Unmap() // treat ::ffff:192.168.1.5 like 192.168.1.5
	return a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast()
}

// sameOrigin reports whether a request was made by this app's own page.
// Browsers mark requests that another website or another web app on this PC
// makes on the visitor's behalf: with Sec-Fetch-Site, or in older browsers
// with Origin. Requests from tools such as curl carry neither and pass; they
// do not run in the visitor's logged-in browser.
func sameOrigin(r *http.Request) bool {
	if s := r.Header.Get("Sec-Fetch-Site"); s != "" {
		return s == "same-origin"
	}
	o := r.Header.Get("Origin")
	return o == "" || o == "https://"+r.Host
}

// guard is the outermost handler. It rejects clients that are not on the LAN,
// as a second line of defense behind the firewall, and tells browsers not to
// guess content types.
//
// Every POST (login, logout) must come from this app's own page. Otherwise a
// website the visitor opens could use their browser, which is on the LAN, to
// guess the password. Its body must also arrive within 10 seconds, so a client
// cannot hold a connection open forever by sending it slowly.
func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLAN(r.RemoteAddr) {
			http.Error(w, "LAN only", http.StatusForbidden)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method == http.MethodPost {
			if !sameOrigin(r) {
				http.Error(w, "cross-site request refused", http.StatusForbidden)
				return
			}
			http.NewResponseController(w).SetReadDeadline(time.Now().Add(10 * time.Second))
		}
		next.ServeHTTP(w, r)
	})
}

// sign returns a session token that expires at the Unix time exp. The token
// has the form "<exp>.<hex HMAC-SHA256 of exp>". It is stateless: the server
// stores nothing and only needs the secret to check a token later.
func (s *server) sign(exp int64) string {
	e := strconv.FormatInt(exp, 10)
	m := hmac.New(sha256.New, s.secret)
	m.Write([]byte(e))
	return e + "." + hex.EncodeToString(m.Sum(nil))
}

// valid reports whether tok is a token made by sign that has not expired.
// It recomputes the token for the claimed expiry and compares in constant
// time, so a forged or edited token fails.
func (s *server) valid(tok string) bool {
	e, _, ok := strings.Cut(tok, ".")
	exp, err := strconv.ParseInt(e, 10, 64)
	if !ok || err != nil || time.Now().Unix() > exp {
		return false
	}
	return hmac.Equal([]byte(tok), []byte(s.sign(exp)))
}

// page answers with the games for a logged-in browser and with the login form
// for everyone else. One URL serves two different pages, so neither may be
// cached: otherwise the login form could reappear after logging in.
func (s *server) page(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if c, err := r.Cookie(cookieName); err == nil && s.valid(c.Value) {
		w.Write(indexHTML)
		return
	}
	w.Write(loginHTML)
}

// login checks the password sent in the form field "pw" and, if it is right,
// sets the session cookie. Either way it redirects back to the page; after a
// wrong password the address is /?wrong, which makes the form say so.
//
// The password is hashed first so the comparison always works on two
// fixed-length values and takes the same time for any input. loginMu is held
// while sleeping after a wrong password, which means failed attempts are
// handled one at a time. Wrong passwords are logged with the client's
// address, at most one a second because of the lock.
func (s *server) login(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	h := sha256.Sum256([]byte(r.PostForm.Get("pw")))
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	if subtle.ConstantTimeCompare(h[:], s.pwHash[:]) != 1 {
		log.Printf("wrong password from %s", r.RemoteAddr)
		time.Sleep(s.delay)
		http.Redirect(w, r, "/?wrong", http.StatusSeeOther)
		return
	}
	// HttpOnly: scripts cannot read it. Secure: only sent over HTTPS.
	// SameSite=Lax: not sent on cross-site requests.
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: s.sign(time.Now().Add(sessionTTL).Unix()), Path: "/",
		MaxAge: int(sessionTTL.Seconds()), HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// logout deletes the session cookie in the browser that asks (MaxAge -1).
//
// Sessions are stateless, so the server cannot cancel a token: a copy of the
// cookie kept elsewhere stays valid until it expires. Restarting the server
// ends every session.
func logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Path: "/", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// env returns the environment variable key, or def if it is unset or empty.
func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// main reads the configuration and starts the HTTPS server.
func main() {
	pw := os.Getenv("BT_PASSWORD")
	if pw == "" {
		log.Fatal("BT_PASSWORD is required")
	}
	home, _ := os.UserHomeDir()
	cfg := filepath.Join(home, ".config", "braintraining")
	s, err := newServer(pw)
	if err != nil {
		log.Fatal(err)
	}
	srv := &http.Server{
		Addr:              env("BT_ADDR", ":8444"),
		Handler:           s.routes(),
		TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS12},
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	log.Printf("serving on https://%s", srv.Addr)
	log.Fatal(srv.ListenAndServeTLS(env("BT_CERT", filepath.Join(cfg, "cert.pem")), env("BT_KEY", filepath.Join(cfg, "key.pem"))))
}
