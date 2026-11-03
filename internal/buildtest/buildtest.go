// Package buildtest builds a game's datastore end to end from the small
// fixture under the builder's testdata: the builder's own re-read holds the
// build to the game's invariants, the same inputs must give the same bytes,
// and the document must match a golden copy, so a change to what is
// published shows up as a diff.
//
// The builder is the binary, built and run as the publish runs it, with
// every upstream pinned to a file and a dead proxy in its environment, so a
// fetch the fixture forgot to pin stops the builder and fails the test
// instead of reaching out, or retrying and carrying on without it.
package buildtest

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/golden.json from this build")

// Golden is where every fixture keeps the document it is expected to build.
const Golden = "testdata/golden.json"

// deadProxy is where a fetch the fixture did not pin is sent: nothing
// listens on the discard port, and the builder is stopped the moment it
// tries.
const (
	deadProxyAddress = "127.0.0.1:9"
	deadProxy        = "http://" + deadProxyAddress
)

// Run builds the game from testdata/tcgplayer-catalog.json and the upstream
// flags given, which pin every upstream to a file under testdata, twice, and
// compares the build to the golden copy.
func Run(t *testing.T, game string, upstreams ...string) {
	t.Helper()
	dir := t.TempDir()
	binary := filepath.Join(dir, game)
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go command to build the builder with")
	}
	if out, err := exec.Command(gobin, "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	args := append([]string{"-tcg-catalog", "testdata/tcgplayer-catalog.json"}, upstreams...)
	first := holdDate(t, runBuilder(t, binary, game, append(args, "-o", filepath.Join(dir, "first.json"))))
	second := holdDate(t, runBuilder(t, binary, game, append(args, "-o", filepath.Join(dir, "second.json"))))
	if !bytes.Equal(first, second) {
		t.Error("two builds of the same inputs differ")
	}
	checkGolden(t, first)
}

// runBuilder runs the built binary and returns what it wrote.
func runBuilder(t *testing.T, binary, game string, args []string) []byte {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Env = append(os.Environ(),
		"HTTP_PROXY="+deadProxy, "HTTPS_PROXY="+deadProxy, "NO_PROXY=",
		"http_proxy="+deadProxy, "https_proxy="+deadProxy, "no_proxy=")
	stderr := &proxyWatch{cmd: cmd}
	cmd.Stderr = stderr
	err := cmd.Run()
	if line := stderr.reached(); line != "" {
		t.Fatalf("%s reached for the network: an upstream is not pinned\n%s", game, line)
	}
	if err != nil {
		lines := strings.Split(strings.TrimSpace(stderr.log.String()), "\n")
		t.Fatalf("%s refused the build: %s\n%s: %v\n%s", game, lines[len(lines)-1],
			strings.Join(args, " "), err, stderr.log.String())
	}
	written, err := os.ReadFile(args[len(args)-1])
	if err != nil {
		t.Fatal(err)
	}
	return written
}

// proxyWatch is a builder's log, which stops the builder the first time it
// reaches for the dead proxy: an upstream the fixture left unpinned. Some
// upstreams are optional, and a builder retrying one for fourteen seconds
// and carrying on without it would otherwise pass.
type proxyWatch struct {
	// log is a field rather than embedded: an embedded Buffer's ReadFrom
	// would let io.Copy fill it without ever calling Write.
	log     bytes.Buffer
	cmd     *exec.Cmd
	stopped bool
}

// Write keeps the log and stops the builder at the dead proxy.
func (w *proxyWatch) Write(p []byte) (int, error) {
	n, err := w.log.Write(p)
	if !w.stopped && bytes.Contains(w.log.Bytes(), []byte(deadProxyAddress)) {
		w.stopped = true
		_ = w.cmd.Process.Kill()
	}
	return n, err
}

// reached is the log line that names the dead proxy, "" where none does.
func (w *proxyWatch) reached() string {
	for line := range strings.Lines(w.log.String()) {
		if strings.Contains(line, deadProxyAddress) {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

// dateRe is meta.date, which is the first date key in the file because
// meta is encoded before data.
var dateRe = regexp.MustCompile(`"date":\s*"[^"]*"`)

// holdDate is a build with meta.date held still: the one part of a build
// that changes without its inputs, at midnight UTC.
func holdDate(t *testing.T, built []byte) []byte {
	t.Helper()
	at := dateRe.FindIndex(built)
	if at == nil {
		t.Fatal("no meta.date in the build")
	}
	return slices.Concat(built[:at[0]], []byte(`"date":"0000-00-00"`), built[at[1]:])
}

// rowWidth is the longest value the golden copy writes on one line: every
// card and sealed product fits, so a change to one is a one-line diff.
const rowWidth = 2000

// checkGolden compares the build with testdata/golden.json, laid out a row
// to a line, or rewrites it under -update.
func checkGolden(t *testing.T, built []byte) {
	t.Helper()
	var compact bytes.Buffer
	if err := json.Compact(&compact, built); err != nil {
		t.Fatal(err)
	}
	out := append(layOut(nil, compact.Bytes(), ""), '\n')
	if *update {
		if err := os.WriteFile(Golden, out, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	golden, err := os.ReadFile(Golden)
	if err != nil {
		t.Fatalf("%v; run go test -update to write it", err)
	}
	want, got := bytes.Split(golden, []byte("\n")), bytes.Split(out, []byte("\n"))
	for i := range max(len(want), len(got)) {
		if i >= len(want) || i >= len(got) || !bytes.Equal(want[i], got[i]) {
			t.Errorf("the build differs from %s at line %d:\n  want %s\n  got  %s\nrun go test -update and review the diff",
				Golden, i+1, line(want, i), line(got, i))
			return
		}
	}
}

// line is a document's line i, trimmed, "" past its end.
func line(lines [][]byte, i int) []byte {
	if i >= len(lines) {
		return nil
	}
	return bytes.TrimSpace(lines[i])
}

// layOut appends a compact JSON value on one line where it fits in rowWidth,
// and otherwise one member or element to a line, each laid out the same way.
func layOut(out, value []byte, pad string) []byte {
	if len(value) <= rowWidth || value[0] != '{' && value[0] != '[' {
		return append(out, value...)
	}
	out = append(out, value[0])
	for i, member := range members(value) {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, "\n"+pad+"  "...)
		if value[0] == '{' {
			colon := keyEnd(member)
			out = append(out, member[:colon+1]...)
			member = member[colon+1:]
		}
		out = layOut(out, member, pad+"  ")
	}
	return append(append(out, "\n"+pad...), value[len(value)-1])
}

// members splits a compact object or array into its top-level members.
func members(value []byte) [][]byte {
	var out [][]byte
	depth, start, quoted := 0, 1, false
	for i := 1; i < len(value)-1; i++ {
		switch c := value[i]; {
		case quoted && c == '\\':
			i++
		case c == '"':
			quoted = !quoted
		case quoted:
		case c == '{' || c == '[':
			depth++
		case c == '}' || c == ']':
			depth--
		case c == ',' && depth == 0:
			out = append(out, value[start:i])
			start = i + 1
		}
	}
	if start < len(value)-1 {
		out = append(out, value[start:len(value)-1])
	}
	return out
}

// keyEnd is the colon that ends an object member's key.
func keyEnd(member []byte) int {
	for i := 1; i < len(member); i++ {
		switch member[i] {
		case '\\':
			i++
		case '"':
			return i + 1
		}
	}
	return len(member) - 1
}
