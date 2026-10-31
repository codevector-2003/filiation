//go:build !windows

package main

import "os"

// enableColour has nothing to do outside Windows: terminals there read ANSI
// colour codes already.
func enableColour(*os.File) bool { return true }
