// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package localfs

import (
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// PreviewDir gives the viewer a private directory that survives process exit.
func (f Files) PreviewDir() (string, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", err
	}
	// Protect the DACL from the temp parent's permissions, which may grant
	// access to other users (for example in MSYS2). Children inherit this
	// user's full access. Supply it at creation so no unprotected window exists.
	sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		return "", err
	}
	sa := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	dir := f.Dir
	if dir == "" {
		dir = os.TempDir()
	}
	apiDir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	// CreateDirectory does not have os.Mkdir's automatic long-path handling.
	// Use an extended absolute path for both drive and UNC temp directories.
	if !strings.HasPrefix(apiDir, `\\?\`) {
		if unc, ok := strings.CutPrefix(apiDir, `\\`); ok {
			apiDir = `\\?\UNC\` + unc
		} else {
			apiDir = `\\?\` + apiDir
		}
	}
	const attempts = 100
	for range attempts {
		base := "snctl-preview-" + rand.Text()
		name := filepath.Join(dir, base)
		p, nameErr := windows.UTF16PtrFromString(filepath.Join(apiDir, base))
		if nameErr != nil {
			return "", nameErr
		}
		err = windows.CreateDirectory(p, &sa)
		if err == nil {
			return name, nil
		}
		if !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
			return "", &os.PathError{Op: "mkdir", Path: name, Err: err}
		}
	}
	return "", &os.PathError{Op: "mkdirtemp", Path: dir, Err: os.ErrExist}
}
