// Command codemap is the standalone development entry point for the indexer.
package main

import (
	"fmt"
	"github.com/amitbet/pr-manager/codemap/indexer"
	"os"
)

func main() {
	if err := indexer.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "codemap:", err)
		os.Exit(1)
	}
}
