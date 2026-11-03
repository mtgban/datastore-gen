package emit

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// upstreamClient bounds a fetch from an upstream list: a hung upstream hangs
// the publish otherwise, since http.Get waits forever. The header deadline
// is the shorter one, so a host that has stopped answering costs an attempt
// half a minute while a slow answer to a 77 MB list still has three.
var upstreamClient = newUpstreamClient()

func newUpstreamClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 30 * time.Second
	return &http.Client{Timeout: 3 * time.Minute, Transport: transport}
}

// userAgent names this build in an upstream's logs, so the mirror can tell
// it from a browser.
const userAgent = "datastore-gen/1.0 (+https://github.com/mtgban/datastore-gen)"

// An upstream is asked four times, waiting 2, 4 and 8 seconds between, on a
// network error, a 5xx or a 429: pokemontcg.io answered 500, 502, 200 to
// three consecutive requests one afternoon. Any other status is the
// server's settled answer and is not asked again.
const (
	fetchAttempts  = 4
	fetchFirstWait = 2 * time.Second
)

// sleep is time.Sleep, swapped out by the tests.
var sleep = time.Sleep

func retryable(status int) bool {
	return status >= 500 || status == http.StatusTooManyRequests
}

// Upstream is one list a build reads over the wire: how to tell an answer
// from an error page, and where the last good one is kept. The zero value
// takes any 200 and keeps nothing.
type Upstream struct {
	// Source names the list in the log ("gcg-api", "tcgdex"); the location
	// stands in when it is empty.
	Source string
	// Check says whether a response is one the build can read: it decodes
	// into the struct the build reads and lists something. A 200 is not an
	// answer. GraphQL reports a failed query as a 200 carrying errors, and
	// a proxy can serve an HTML page as one; written to the cache unread,
	// either poisons the fallback for every run until a live fetch
	// succeeds. A nil Check takes any 200.
	Check func([]byte) error
	// CacheDir is the directory the last good response is kept in as
	// CacheName, or "" to keep none.
	CacheDir, CacheName string
}

// Fetch reads a local path, or an http(s) location when one is given, so a
// build can be pinned to a file and the default can be the live URL. A
// location is asked with retries and, when a cache is given, the last good
// response stands in for one that will not answer.
func (u Upstream) Fetch(location string) ([]byte, error) {
	if !IsURL(location) {
		return os.ReadFile(location)
	}
	return u.Cached(func() ([]byte, error) {
		return retrying(u.name(location), func() (*http.Request, error) {
			return http.NewRequest(http.MethodGet, location, nil)
		})
	})
}

// Post sends a JSON body to an API, the way Fetch asks a location: retried,
// cached, and checked before it is kept. The body is sent afresh on every
// attempt.
func (u Upstream) Post(url string, body []byte) ([]byte, error) {
	return u.Cached(func() ([]byte, error) {
		return retrying(u.name(url), func() (*http.Request, error) {
			req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
			if err != nil {
				return nil, err
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json")
			return req, nil
		})
	})
}

// Cached puts the last-good cache in front of a fetch. The fetch is asked
// first and its answer refreshes the cache, so the cache is always the
// newest response that ever arrived; when the fetch fails the cached
// response stands in, dated out loud, rather than the annotation being
// lost. A day-old answer is strictly better than none, and the baseline the
// build is compared against does not move on a cached run unless the build
// still grew.
//
// The cache only spans runs if the directory does. On a fresh CI workspace
// it is empty every time, so the publish carries the file in and out of
// the bucket; locally the directory simply persists.
func (u Upstream) Cached(fetch func() ([]byte, error)) ([]byte, error) {
	data, err := fetch()
	if err == nil && u.Check != nil {
		if cerr := u.Check(data); cerr != nil {
			err = fmt.Errorf("%s answered, but not with a response this build can read: %w", u.name(""), cerr)
		}
	}
	if err == nil {
		if u.CacheDir != "" {
			if werr := os.WriteFile(u.cachePath(), data, 0o644); werr != nil {
				log.Printf("%s cache: %v (the build continues on the live answer)", u.name(""), werr)
			}
		}
		return data, nil
	}
	if u.CacheDir == "" {
		return nil, err
	}
	info, serr := os.Stat(u.cachePath())
	if serr != nil {
		return nil, err
	}
	blob, rerr := os.ReadFile(u.cachePath())
	if rerr != nil {
		return nil, err
	}
	if u.Check != nil {
		if cerr := u.Check(blob); cerr != nil {
			return nil, fmt.Errorf("%v; and the cached response does not read either: %w", err, cerr)
		}
	}
	log.Printf("%s unreachable (%v); using the cached response from %s",
		u.name(""), err, info.ModTime().UTC().Format("2006-01-02 15:04"))
	return blob, nil
}

func (u Upstream) cachePath() string {
	return filepath.Join(u.CacheDir, u.CacheName)
}

func (u Upstream) name(location string) string {
	switch {
	case u.Source != "":
		return u.Source
	case location != "":
		return location
	}
	return "upstream"
}

// Fetch is the zero Upstream's: a path or a location, retried, with no
// cache and any 200 taken.
func Fetch(location string) ([]byte, error) {
	return Upstream{}.Fetch(location)
}

// IsURL says whether a flag's value names an http(s) location rather than
// a file.
func IsURL(location string) bool {
	return strings.HasPrefix(location, "http://") || strings.HasPrefix(location, "https://")
}

// Reads is a Check that decodes a response into T and asks count whether
// it lists anything, so the one line a builder writes is its struct.
func Reads[T any](count func(T) int) func([]byte) error {
	return func(data []byte) error {
		var v T
		if err := json.Unmarshal(data, &v); err != nil {
			return err
		}
		if count(v) == 0 {
			return errors.New("the response lists nothing")
		}
		return nil
	}
}

// retrying asks the request build returns until one attempt answers 200,
// a status that will not change on a retry comes back, or the attempts
// run out. build is called afresh each time, so a body is re-sent whole.
func retrying(source string, build func() (*http.Request, error)) ([]byte, error) {
	var lastErr error
	wait := fetchFirstWait
	for attempt := 0; attempt < fetchAttempts; attempt++ {
		if attempt > 0 {
			log.Printf("%s: %v; retrying in %v", source, lastErr, wait)
			sleep(wait)
			wait *= 2
		}
		req, err := build()
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", userAgent)
		resp, err := upstreamClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("%s: HTTP %d", req.URL, resp.StatusCode)
			if !retryable(resp.StatusCode) {
				return nil, lastErr
			}
			continue
		}
		return data, nil
	}
	return nil, lastErr
}
