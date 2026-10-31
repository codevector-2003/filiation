package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// enableColour turns on the console's handling of ANSI colour codes, off by
// default in the classic Windows console. Windows Terminal has it on already.
// False means the console cannot do it, and fil prints plain.
func enableColour(f *os.File) bool {
	h := windows.Handle(f.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		return false
	}
	if mode&windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING != 0 {
		return true
	}
	return windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING) == nil
}
