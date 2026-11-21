// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license in the LICENSE file.

package localfs_test

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"github.com/altessa-s/snctl/internal/adapter/localfs"

	"golang.org/x/sys/windows"
)

func TestPreviewWindowsACL(t *testing.T) {
	parent := t.TempDir()
	// Model a shared MSYS2 temp directory: Everyone has inheritable access.
	sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(parent, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	for _, systemTemp := range []bool{false, true} {
		t.Run(map[bool]string{false: "explicit", true: "system temp"}[systemTemp], func(t *testing.T) {
			f := localfs.Files{Dir: parent}
			if systemTemp {
				t.Setenv("TMP", parent)
				t.Setenv("TEMP", parent)
				f.Dir = ""
			}
			dir, err := f.PreviewDir()
			if err != nil {
				t.Fatal(err)
			}
			assertPreviewACL(t, dir, user.User.Sid, true)
			file := filepath.Join(dir, "original.pdf")
			if err := f.Produce(t.Context(), file, func(w io.Writer) error { _, err := io.WriteString(w, "private document"); return err }); err != nil {
				t.Fatal(err)
			}
			assertPreviewACL(t, file, user.User.Sid, false)
			if data, err := os.ReadFile(file); err != nil || string(data) != "private document" {
				t.Fatal(string(data), err)
			}
		})
	}
}

func assertPreviewACL(t *testing.T, name string, user *windows.SID, directory bool) {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(name, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := sd.Control()
	if err != nil || directory && control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatal("preview directory must reject inherited parent permissions", control, err)
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil || acl.AceCount != 1 {
		t.Fatal("want exactly one user access entry", acl, err)
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(acl, 0, &ace); err != nil {
		t.Fatal(err)
	}
	sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
	const fileAllAccess = 0x1f01ff
	if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || !windows.EqualSid(sid, user) || ace.Mask != fileAllAccess {
		t.Fatalf("unexpected preview access: SID %s, ACE %+v", sid, ace)
	}
	if directory && ace.Header.AceFlags != windows.OBJECT_INHERIT_ACE|windows.CONTAINER_INHERIT_ACE {
		t.Fatal("preview files and subdirectories must inherit user access", ace.Header)
	}
	if !directory && ace.Header.AceFlags&windows.INHERITED_ACE == 0 {
		t.Fatal("preview file did not inherit user access", ace.Header)
	}
}

func TestPreviewWindowsLongPath(t *testing.T) {
	parent := filepath.Join(t.TempDir(), strings.Repeat("long", 50))
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	dir, err := (localfs.Files{Dir: parent}).PreviewDir()
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatal(info, err)
	}
}
