package main

import (
	"fyne.io/systray"
	"github.com/somprasongd/go-thai-smartcard/pkg/ctl"
	"github.com/somprasongd/go-thai-smartcard/pkg/model"
	"sync"
	"testing"
)

func TestConcurrentServiceMenuRefresh(t *testing.T) {
	tray := &tray{
		mAgent: &systray.MenuItem{}, mAgentToggle: &systray.MenuItem{},
		mAgentRestart: &systray.MenuItem{},
		serviceState:  func() (ctl.State, error) { return ctl.StateRunning, nil },
	}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Go(func() {
			for j := 0; j < 20; j++ {
				tray.refreshServiceState()
			}
		})
	}
	wg.Wait()
}

func TestConcurrentConnectionAndServiceUpdates(t *testing.T) {
	tr := &tray{
		mStatus: &systray.MenuItem{}, mAgent: &systray.MenuItem{},
		mAgentToggle: &systray.MenuItem{}, mAgentRestart: &systray.MenuItem{},
		serviceState: func() (ctl.State, error) { return ctl.StateRunning, nil },
	}
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := 0; i < 30; i++ {
			tr.refreshServiceState()
		}
	})
	wg.Go(func() {
		for i := 0; i < 30; i++ {
			tr.connectionState("connected")
			tr.applyStatus(map[string]any{"state": model.StateCardPresent})
		}
	})
	wg.Go(func() {
		for i := 0; i < 30; i++ {
			tr.setEndpoint("http://127.0.0.1:9900")
		}
	})
	wg.Wait()
}

func TestBusyServiceActionDoesNotQueueToggle(t *testing.T) {
	m := &testService{state: ctl.StateRunning}
	tr := &tray{manager: m}
	tr.serviceOps.Lock()
	// A queued action would block here and could resume a just-paused service.
	tr.serviceAction("toggle")
	tr.serviceOps.Unlock()
	if m.starts != 0 || m.stops != 0 {
		t.Fatal("busy action changed service")
	}
}
