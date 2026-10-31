// Command esa is the easy-sys-admin binary: control plane (serve), server helper, and CLI.
package main

import (
	"os"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
