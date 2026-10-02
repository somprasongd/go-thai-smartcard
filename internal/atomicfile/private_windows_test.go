//go:build windows

package atomicfile

import (
	"golang.org/x/sys/windows"
	"path/filepath"
	"testing"
	"unsafe"
)

func TestPrivateFilesUseProtectedWindowsACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private")
	f, err := OpenLock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := sd.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatalf("ACL is not protected: %v", err)
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil || acl.AceCount != 3 {
		t.Fatalf("unexpected ACL: %v", err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		t.Fatal(err)
	}
	admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		t.Fatal(err)
	}
	expected := []*windows.SID{system, admins, user.User.Sid}
	seen := make([]bool, len(expected))
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(acl, i, &ace); err != nil {
			t.Fatal(err)
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			t.Fatal("unexpected deny/object ACE")
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		matched := false
		for j, want := range expected {
			if windows.EqualSid(sid, want) {
				seen[j] = true
				matched = true
				break
			}
		}
		if !matched {
			t.Fatal("ACL grants an unexpected principal")
		}
	}
	for _, ok := range seen {
		if !ok {
			t.Fatal("ACL missing an intended principal")
		}
	}
}
