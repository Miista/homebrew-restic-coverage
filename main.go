// restic-coverage audits that everything on disk is deliberately backed up,
// excluded, or ignored (with a reason) by a resticprofile setup.
package main

import (
	"os"

	"restic-coverage/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout))
}
