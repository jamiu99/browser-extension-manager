package main

import (
	"fmt"
	"os"

	"github.com/jamiu99/browser-extension-manager/internal/cli"
	"github.com/jamiu99/browser-extension-manager/internal/manager"
)

func main() {
	dir, _, err := manager.DefaultPaths()
	if err == nil {
		err = cli.Run(manager.BrowserSettings{Dir: dir}, os.Args[1:], os.Stdout, os.Stderr)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
