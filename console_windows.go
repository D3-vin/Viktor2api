//go:build windows

package main

import (
	"os"
	"syscall"
)

var kernel32 = syscall.NewLazyDLL("kernel32.dll")
var setConsoleCtrlHandler = kernel32.NewProc("SetConsoleCtrlHandler")

const (
	ctrlCloseEvent    = 2
	ctrlLogoffEvent   = 5
	ctrlShutdownEvent = 6
)

// registerConsoleCloseHandler ensures the process exits when the console window is closed.
func registerConsoleCloseHandler(quit chan os.Signal) {
	cb := syscall.NewCallback(func(ctrlType uint32) uintptr {
		switch ctrlType {
		case ctrlCloseEvent, ctrlLogoffEvent, ctrlShutdownEvent:
			quit <- os.Interrupt
			// Block briefly so the signal handler has time to run before Windows kills us
			select {}
		}
		return 0
	})
	setConsoleCtrlHandler.Call(cb, 1)
}
