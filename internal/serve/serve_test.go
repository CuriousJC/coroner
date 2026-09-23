package serve

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/curiousjc/coroner/internal/timeline"
)

func TestCheckLoopback(t *testing.T) {
	for _, ok := range []string{"127.0.0.1:8484", "localhost:8484", "[::1]:8484", "127.0.0.2:80"} {
		if err := CheckLoopback(ok); err != nil {
			t.Errorf("%s refused: %v", ok, err)
		}
	}
	for _, bad := range []string{":8484", "0.0.0.0:8484", "[::]:8484", "192.168.1.10:8484", "example.com:80", "8484"} {
		if err := CheckLoopback(bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

func sample() timeline.Timeline {
	return timeline.Timeline{
		Sources:   []timeline.Source{{Name: "site", Type: "htmlsite", Entries: 1}},
		Documents: 1,
		Entries: []timeline.Entry{{
			ID: "abc", Source: "site", Type: "htmlsite", Title: "First Post",
			Published: time.Date(2016, 10, 25, 0, 0, 0, 0, time.UTC), Words: 3, Text: "The first post.",
		}},
	}
}

var listen = &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8484}

func get(t *testing.T, h http.Handler, method, host, path string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, "http://"+host+path, nil)
	req.Host = host
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Result()
}

// A page elsewhere can point its own hostname at 127.0.0.1; the browser then
// sends that hostname, and the request must be refused.
func TestRefusesRequestsNotAddressedToLoopback(t *testing.T) {
	h, err := Handler(sample(), nil, listen)
	if err != nil {
		t.Fatal(err)
	}

	for _, host := range []string{"127.0.0.1:8484", "localhost:8484", "[::1]:8484"} {
		if res := get(t, h, "GET", host, DataPath); res.StatusCode != http.StatusOK {
			t.Errorf("Host %s: status %d", host, res.StatusCode)
		}
	}
	for _, host := range []string{"evil.example:8484", "127.0.0.1:9999", "localhost"} {
		if res := get(t, h, "GET", host, DataPath); res.StatusCode != http.StatusMisdirectedRequest {
			t.Errorf("Host %s: status %d, want %d", host, res.StatusCode, http.StatusMisdirectedRequest)
		}
	}
}

func TestServesTheTimelineAsJSON(t *testing.T) {
	h, err := Handler(sample(), nil, listen)
	if err != nil {
		t.Fatal(err)
	}

	res := get(t, h, "GET", "127.0.0.1:8484", DataPath)
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content type %q", ct)
	}
	if res.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Error("a CORS header would let another origin read the corpus")
	}
	if !strings.Contains(res.Header.Get("Content-Security-Policy"), "default-src 'self'") {
		t.Error("no content security policy")
	}

	var tl timeline.Timeline
	if err := json.NewDecoder(res.Body).Decode(&tl); err != nil {
		t.Fatal(err)
	}
	if len(tl.Entries) != 1 || tl.Entries[0].Title != "First Post" {
		t.Errorf("entries = %+v", tl.Entries)
	}
}

// Built without the front end, the root is the static timeline page.
func TestFallsBackToTheStaticPage(t *testing.T) {
	h, err := Handler(sample(), nil, listen)
	if err != nil {
		t.Fatal(err)
	}

	res := get(t, h, "GET", "127.0.0.1:8484", "/")
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), "The first post.") {
		t.Errorf("status %d, body %.200q", res.StatusCode, body)
	}
}

func TestServesTheFrontEndWhenBuiltIn(t *testing.T) {
	ui := fstest.MapFS{"index.html": {Data: []byte("<!doctype html><div id=root></div>")}}
	h, err := Handler(sample(), ui, listen)
	if err != nil {
		t.Fatal(err)
	}

	res := get(t, h, "GET", "127.0.0.1:8484", "/")
	body, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(body), `id=root`) {
		t.Errorf("root served %.200q", body)
	}
}

// Nothing here changes anything; only reads are answered.
func TestAnswersOnlyReads(t *testing.T) {
	h, err := Handler(sample(), nil, listen)
	if err != nil {
		t.Fatal(err)
	}
	if res := get(t, h, "POST", "127.0.0.1:8484", DataPath); res.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST: status %d", res.StatusCode)
	}
}
