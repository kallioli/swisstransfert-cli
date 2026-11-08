// Command swisstransfer is an unofficial command-line client for SwissTransfer.
package main

import (
	"os"

	"github.com/kallioli/swisstransfert-cli/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr, os.Stdin))
}
