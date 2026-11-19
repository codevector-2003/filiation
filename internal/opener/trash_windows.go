//go:build windows

package opener

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

// The Recycle Bin is reached through SHFileOperationW, as Explorer's own
// Delete is, loaded from shell32 at run time: no cgo (hard rule 4).
var procFileOperation = syscall.NewLazyDLL("shell32.dll").NewProc("SHFileOperationW")

// shFileOpStruct is SHFILEOPSTRUCTW. On 64-bit Windows, the only Windows fil
// ships for, its fields take their natural alignment, as Go lays them out.
type shFileOpStruct struct {
	hwnd                  uintptr
	wFunc                 uint32
	pFrom                 *uint16
	pTo                   *uint16
	fFlags                uint16
	fAnyOperationsAborted int32
	hNameMappings         uintptr
	lpszProgressTitle     *uint16
}

const (
	foDelete          = 0x3
	fofSilent         = 0x4   // no progress window
	fofNoConfirmation = 0x10  // fil has asked already
	fofAllowUndo      = 0x40  // to the Recycle Bin, not deleted outright
	fofNoErrorUI      = 0x400 // fil reports the error itself
)

func trash(path string) error {
	from, err := syscall.UTF16FromString(path)
	if err != nil {
		return err
	}
	from = append(from, 0) // the list of paths ends with a second NUL
	op := shFileOpStruct{
		wFunc:  foDelete,
		pFrom:  &from[0],
		fFlags: fofSilent | fofNoConfirmation | fofAllowUndo | fofNoErrorUI,
	}
	r, _, _ := procFileOperation.Call(uintptr(unsafe.Pointer(&op)))
	switch {
	case r != 0:
		return fmt.Errorf("the Recycle Bin refused it (code %#x)", r)
	case op.fAnyOperationsAborted != 0:
		return errors.New("moving it to the Recycle Bin was cancelled")
	}
	return nil
}
