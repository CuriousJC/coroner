// Package serve is the HTTP side of `coroner serve`: the timeline as JSON, and
// either the built front end or the static timeline page to read it with.
//
// Everything served is the private corpus, so the server is built to be
// reachable from this machine and nothing else:
//
//   - It listens on loopback only. CheckLoopback refuses any other address, and
//     there is no flag to override it.
//   - It answers only requests addressed to loopback by name. A page on some
//     other site can point a hostname it controls at 127.0.0.1 and have the
//     browser fetch from here (DNS rebinding); the Host header still names that
//     site, so the request is refused.
//   - It sends no CORS headers, so no other origin can read a response, and a
//     Content-Security-Policy that stops the front end itself reaching out.
package serve

import (
	"bytes"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"strconv"

	"github.com/curiousjc/coroner/internal/timeline"
)

// DefaultAddr is where `coroner serve` listens unless told otherwise.
const DefaultAddr = "127.0.0.1:8484"

// DataPath is where the front end fetches the timeline from.
const DataPath = "/api/writing.json"

// CheckLoopback refuses a listen address that is not on loopback. An empty host
// or 0.0.0.0 would listen on every interface, which for a server holding the
// whole corpus means the local network can read it.
func CheckLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("listen address %q: %w", addr, err)
	}
	if host == "localhost" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("refusing to listen on %q: coroner serve only listens on loopback (127.0.0.1, ::1 or localhost), because everything it serves is the private corpus", addr)
}

// Handler serves the timeline. listen is the address actually bound, whose port
// is what legitimate requests carry in their Host header. ui is the built front
// end; nil serves the static timeline page instead.
func Handler(tl timeline.Timeline, ui fs.FS, listen net.Addr) (http.Handler, error) {
	var data bytes.Buffer
	if err := tl.JSON(&data); err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET "+DataPath, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Write(data.Bytes())
	})

	if ui != nil {
		mux.Handle("GET /", http.FileServerFS(ui))
	} else {
		var page bytes.Buffer
		if err := tl.HTML(&page); err != nil {
			return nil, err
		}
		mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(page.Bytes())
		})
	}

	port := ""
	if tcp, ok := listen.(*net.TCPAddr); ok {
		port = strconv.Itoa(tcp.Port)
	}
	return guard(mux, port), nil
}

// csp keeps the front end to its own origin: it can load its own scripts and
// styles and fetch the timeline, and cannot reach anything else.
const csp = "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; " +
	"base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

// guard refuses requests not addressed to this server by a loopback name, and
// sets the headers every response carries.
func guard(next http.Handler, port string) http.Handler {
	allowed := map[string]bool{
		net.JoinHostPort("127.0.0.1", port): true,
		net.JoinHostPort("localhost", port): true,
		net.JoinHostPort("::1", port):       true,
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowed[r.Host] {
			http.Error(w, "coroner serve only answers requests addressed to loopback", http.StatusMisdirectedRequest)
			return
		}

		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
