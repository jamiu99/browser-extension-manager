//go:build !windows

package main

import "github.com/jamiu99/browser-extension-manager/internal/manager"

func windowsAutomation(_ []manager.Browser, _ []manager.Extension, _ bool) []string {
	return nil
}
