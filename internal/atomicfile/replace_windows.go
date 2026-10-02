//go:build windows

package atomicfile

import (
	"errors"
	"golang.org/x/sys/windows"
	"io"
	"os"
	"time"
)

func replace(from, to string) error {
	f, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	t, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(250 * time.Millisecond)
	for {
		err = windows.MoveFileEx(f, t, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
		if (!errors.Is(err, windows.ERROR_SHARING_VIOLATION) && !errors.Is(err, windows.ERROR_ACCESS_DENIED)) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// ReadFile permits replacement while a tray or API reader holds the file.
func ReadFile(path string) ([]byte, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	// A replaced name can briefly refer to an old delete-pending handle even
	// with share-delete enabled. Retry only Windows' transient open failures;
	// persistent permission/missing-file errors still return within a bound.
	var h windows.Handle
	deadline := time.Now().Add(250 * time.Millisecond)
	for {
		h, err = windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
		transient := errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_FILE_NOT_FOUND)
		if err == nil {
			break
		}
		if !transient || time.Now().After(deadline) {
			return nil, err
		}
		time.Sleep(time.Millisecond)
	}
	f := os.NewFile(uintptr(h), path)
	defer f.Close()
	return io.ReadAll(f)
}

func setMode(f *os.File, mode os.FileMode) error {
	if err := f.Chmod(mode); err != nil {
		return err
	}
	if mode.Perm() != 0600 {
		return nil
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;SY)(A;;FA;;;BA)(A;;FA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(f.Name(), windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}
