// Command datastorediff says what one build of a datastore did to another,
// in one line, for the tagging the repository does from its own output.
//
// It is the one command here that builds no datastore. Every other cmd
// ingests a catalog and emits a game's datastore; this one reads two of
// those and reports the difference, so a commit can be tagged by what it
// published rather than by what its message claims.
//
// Both files must have been built from one catalog. Two builds a day apart
// differ by every card TCGplayer added in between, and no amount of
// reporting can tell that from a change the code made.
//
//	datastorediff before.json after.json
//
// It prints "no change" when the two are the same datastore, and exits
// non-zero only when it could not read them - a difference is an answer,
// not a failure.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/mtgban/datastore-gen/internal/datastorediff"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("datastorediff: ")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: datastorediff <before.json> <after.json>")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 2 {
		flag.Usage()
		os.Exit(2)
	}
	before, err := os.ReadFile(flag.Arg(0))
	if err != nil {
		log.Fatalln(err)
	}
	after, err := os.ReadFile(flag.Arg(1))
	if err != nil {
		log.Fatalln(err)
	}
	change, err := datastorediff.Compare(before, after)
	if err != nil {
		log.Fatalln(err)
	}
	// Fprintln rather than Println: the summary is what the caller reads
	// off stdout, and .revive.toml allows a write to a stream it lists
	// while an unchecked Println is an unhandled error.
	fmt.Fprintln(os.Stdout, change)
}
