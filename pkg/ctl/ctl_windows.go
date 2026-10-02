//go:build windows

package ctl

import (
	"fmt"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// The installer grants Interactive Users SERVICE_START and SERVICE_STOP on
// this one service (packaging/windows/installer.iss runs `sc sdset`), so the
// tray talks to the Service Control Manager directly: no elevation, no UAC
// dialog. Manual installs that skipped the installer keep the default ACL and
// the tray's calls fail — the menu then disables itself.
type scmanager struct{}

// New returns the Windows control mechanism.
func New() Manager { return scmanager{} }

func openService(access uint32) (*mgr.Service, func(), error) {
	h, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return nil, nil, fmt.Errorf("connect to the service manager: %w", err)
	}
	name, err := windows.UTF16PtrFromString(serviceName)
	if err != nil {
		windows.CloseServiceHandle(h)
		return nil, nil, err
	}
	sh, err := windows.OpenService(h, name, access)
	if err != nil {
		windows.CloseServiceHandle(h)
		return nil, nil, fmt.Errorf("open %s: %w", serviceName, err)
	}
	s := &mgr.Service{Name: serviceName, Handle: sh}
	return s, func() { s.Close(); windows.CloseServiceHandle(h) }, nil
}

func (scmanager) State() (State, error) {
	s, done, err := openService(windows.SERVICE_QUERY_STATUS)
	if err != nil {
		return StateUnknown, err
	}
	defer done()
	st, err := s.Query()
	if err != nil {
		return StateUnknown, fmt.Errorf("query %s: %w", serviceName, err)
	}
	switch st.State {
	case svc.Running:
		return StateRunning, nil
	case svc.Stopped:
		return StateStopped, nil
	}
	return StateUnknown, nil
}

func (scmanager) Start() error {
	s, done, err := openService(windows.SERVICE_START)
	if err != nil {
		return err
	}
	defer done()
	if err := s.Start(); err != nil {
		return fmt.Errorf("start %s: %w", serviceName, err)
	}
	return nil
}

// stopAndWait issues the stop control and waits for the service to settle,
// bounded: the tray reflects the state on its next poll regardless.
func stopAndWait(s *mgr.Service) error {
	if err := stopOnce(s); err != nil {
		return err
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		st, err := s.Query()
		if err != nil {
			return err
		}
		if st.State == svc.Stopped {
			return nil
		}
		if time.Now().After(deadline) {
			return nil // stop is in flight; the next poll reports it
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func stopOnce(s *mgr.Service) error {
	st, err := s.Query()
	if err != nil {
		return err
	}
	if st.State == svc.Stopped {
		return nil
	}
	if _, err := s.Control(svc.Stop); err != nil {
		return fmt.Errorf("stop %s: %w", serviceName, err)
	}
	return nil
}

func (scmanager) Stop() error {
	s, done, err := openService(windows.SERVICE_QUERY_STATUS | windows.SERVICE_STOP)
	if err != nil {
		return err
	}
	defer done()
	return stopAndWait(s)
}

func (scmanager) Restart() error {
	s, done, err := openService(windows.SERVICE_QUERY_STATUS | windows.SERVICE_STOP | windows.SERVICE_START)
	if err != nil {
		return err
	}
	defer done()
	if err := stopAndWait(s); err != nil {
		return err
	}
	return s.Start()
}
