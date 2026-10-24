// Command exon sets up a freshly reset Mac from a config repo.
package main

import (
	"os"

	"github.com/bitbynik/exon/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
