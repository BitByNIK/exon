// Command exon is personal setup tooling, driven by a config repo you own.
package main

import (
	"os"

	"github.com/bitbynik/exon/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
