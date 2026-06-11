//go:build !windows

package main

import "os"

func registerConsoleCloseHandler(quit chan os.Signal) {
	// No-op on non-Windows platforms — SIGTERM/SIGINT are handled natively
}
