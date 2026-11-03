package emit

import (
	"bytes"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// quietWaits replaces the backoff sleep for one test and returns the waits
// it was asked for.
func quietWaits(t *testing.T) *[]time.Duration {
	t.Helper()
	var waits []time.Duration
	sleep = func(d time.Duration) { waits = append(waits, d) }
	t.Cleanup(func() { sleep = time.Sleep })
	return &waits
}

// capturedLog routes the package log to a buffer for one test.
func capturedLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return &buf
}

// statusServer answers each request with the next status in the list and
// the last one forever after, with a body of the status spelled out.
func statusServer(t *testing.T, statuses ...int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(hits.Add(1)) - 1
		if n >= len(statuses) {
			n = len(statuses) - 1
		}
		w.WriteHeader(statuses[n])
		_, _ = io.WriteString(w, `{"status":`+http.StatusText(statuses[n])+`}`)
	}))
	t.Cleanup(server.Close)
	return server, &hits
}

// TestFetchRetriesWhatMayClear pins the policy: a 5xx or a 429 is asked
// again with a doubling wait, up to four attempts in all, and the first 200
// is the answer.
func TestFetchRetriesWhatMayClear(t *testing.T) {
	waits := quietWaits(t)
	capturedLog(t)
	server, hits := statusServer(t, http.StatusInternalServerError, http.StatusTooManyRequests, http.StatusOK)
	got, err := Fetch(server.URL)
	if err != nil || !strings.Contains(string(got), "OK") {
		t.Fatalf("Fetch = %q, %v; want the third answer", got, err)
	}
	if hits.Load() != 3 {
		t.Errorf("asked %d times, want 3", hits.Load())
	}
	want := []time.Duration{2 * time.Second, 4 * time.Second}
	if len(*waits) != len(want) || (*waits)[0] != want[0] || (*waits)[1] != want[1] {
		t.Errorf("waited %v, want %v", *waits, want)
	}
}

// TestFetchGivesUpAfterFourAttempts pins the ceiling: a server that keeps
// failing is asked four times and the last status is the error.
func TestFetchGivesUpAfterFourAttempts(t *testing.T) {
	quietWaits(t)
	capturedLog(t)
	server, hits := statusServer(t, http.StatusBadGateway)
	_, err := Fetch(server.URL)
	if err == nil || !strings.HasSuffix(err.Error(), ": HTTP 502") {
		t.Errorf("Fetch = %v, want the last status named", err)
	}
	if hits.Load() != fetchAttempts {
		t.Errorf("asked %d times, want %d", hits.Load(), fetchAttempts)
	}
}

// TestFetchDoesNotRetryASettledAnswer pins that a 4xx other than 429 is
// the server's answer: one request, no wait.
func TestFetchDoesNotRetryASettledAnswer(t *testing.T) {
	waits := quietWaits(t)
	for _, status := range []int{http.StatusNotFound, http.StatusForbidden, http.StatusBadRequest} {
		server, hits := statusServer(t, status, http.StatusOK)
		_, err := Fetch(server.URL)
		if err == nil {
			t.Errorf("HTTP %d: Fetch answered, want the status refused", status)
		}
		if hits.Load() != 1 {
			t.Errorf("HTTP %d: asked %d times, want 1", status, hits.Load())
		}
	}
	if len(*waits) != 0 {
		t.Errorf("waited %v on settled answers", *waits)
	}
}

// TestFetchRetriesANetworkError pins that a connection refused is retried
// rather than returned: the second attempt reaches a server the first could
// not.
func TestFetchRetriesANetworkError(t *testing.T) {
	quietWaits(t)
	capturedLog(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"live":true}`)
	}))
	server.Close()
	if _, err := Fetch(server.URL); err == nil {
		t.Fatal("a closed server answered")
	}
	// Each attempt builds a new request, so the error is one of connection,
	// not of the request; the retries are what the log shows.
	buf := capturedLog(t)
	_, _ = Fetch(server.URL)
	if n := strings.Count(buf.String(), "retrying in"); n != fetchAttempts-1 {
		t.Errorf("logged %d retries on a dead host, want %d", n, fetchAttempts-1)
	}
}

// TestPostSendsTheBodyOnEveryAttempt pins the one thing a retried POST can
// get wrong: the body is consumed by the first attempt, so each attempt has
// to carry it afresh.
func TestPostSendsTheBodyOnEveryAttempt(t *testing.T) {
	quietWaits(t)
	capturedLog(t)
	var bodies []string
	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(body))
		hits++
		if hits == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if r.Header.Get("Content-Type") != "application/json" || r.Method != http.MethodPost {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, `{"data":{"ok":true}}`)
	}))
	defer server.Close()
	got, err := Upstream{Source: "graphql"}.Post(server.URL, []byte(`{"query":"{ sets { id } }"}`))
	if err != nil || string(got) != `{"data":{"ok":true}}` {
		t.Fatalf("Post = %q, %v", got, err)
	}
	if len(bodies) != 2 || bodies[0] != bodies[1] || bodies[1] == "" {
		t.Errorf("bodies sent %q, want the same body twice", bodies)
	}
}

// TestCacheStandsInForAnUnreachableUpstream pins the fallback: a live fetch
// that fails on every attempt is answered from the cache, and the log says
// so with the cache's date.
func TestCacheStandsInForAnUnreachableUpstream(t *testing.T) {
	quietWaits(t)
	buf := capturedLog(t)
	server, _ := statusServer(t, http.StatusBadGateway)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rows.json"), []byte(`[{"id":1}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	u := Upstream{
		Source:    "rows",
		Check:     Reads(func(rows []struct{ ID int }) int { return len(rows) }),
		CacheDir:  dir,
		CacheName: "rows.json",
	}
	got, err := u.Fetch(server.URL)
	if err != nil || string(got) != `[{"id":1}]` {
		t.Fatalf("Fetch = %q, %v; want the cached rows", got, err)
	}
	logged := buf.String()
	if !strings.Contains(logged, "rows unreachable") || !strings.Contains(logged, "using the cached response from 20") {
		t.Errorf("log says %q, want the fallback dated", logged)
	}
	// Without a cache the same failure is the error.
	if _, err := (Upstream{Source: "rows", Check: u.Check}).Fetch(server.URL); err == nil {
		t.Error("an unreachable upstream with no cache answered")
	}
}

// TestCacheKeepsOnlyWhatReads pins that the cache holds a response the
// build read, never an error page: a 200 that fails the check is refused
// and leaves the cache as it was, and a 200 that passes refreshes it.
func TestCacheKeepsOnlyWhatReads(t *testing.T) {
	quietWaits(t)
	capturedLog(t)
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, body)
	}))
	defer server.Close()
	dir := t.TempDir()
	u := Upstream{
		Source:    "rows",
		Check:     Reads(func(rows []struct{ ID int }) int { return len(rows) }),
		CacheDir:  dir,
		CacheName: "rows.json",
	}
	body = `<html>502 Bad Gateway</html>`
	if _, err := u.Fetch(server.URL); err == nil || !strings.Contains(err.Error(), "not with a response this build can read") {
		t.Errorf("an error page was taken: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "rows.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the error page reached the cache: %v", err)
	}
	body = `[]`
	if _, err := u.Fetch(server.URL); err == nil {
		t.Error("an empty list was taken")
	}
	body = `[{"id":7}]`
	if got, err := u.Fetch(server.URL); err != nil || string(got) != body {
		t.Fatalf("Fetch = %q, %v", got, err)
	}
	if cached, err := os.ReadFile(filepath.Join(dir, "rows.json")); err != nil || string(cached) != body {
		t.Errorf("cache holds %q, %v; want the answer", cached, err)
	}
	// A cached error page is not an answer either.
	if err := os.WriteFile(filepath.Join(dir, "rows.json"), []byte(`<html>`), 0o600); err != nil {
		t.Fatal(err)
	}
	server.Close()
	if _, err := u.Fetch(server.URL); err == nil || !strings.Contains(err.Error(), "the cached response does not read either") {
		t.Errorf("a cached error page was taken: %v", err)
	}
}

// TestAPinnedFileBypassesTheCache pins that a path on a flag is read as it
// is: no fetch, no cache written, no check.
func TestAPinnedFileBypassesTheCache(t *testing.T) {
	dir := t.TempDir()
	pinned := filepath.Join(dir, "pinned.json")
	if err := os.WriteFile(pinned, []byte(`[]`), 0o600); err != nil {
		t.Fatal(err)
	}
	cache := t.TempDir()
	u := Upstream{
		Source:    "rows",
		Check:     Reads(func(rows []struct{ ID int }) int { return len(rows) }),
		CacheDir:  cache,
		CacheName: "rows.json",
	}
	if got, err := u.Fetch(pinned); err != nil || string(got) != `[]` {
		t.Errorf("Fetch(pinned) = %q, %v; want the file as it is", got, err)
	}
	if entries, _ := os.ReadDir(cache); len(entries) != 0 {
		t.Errorf("a pinned file reached the cache: %v", entries)
	}
}
