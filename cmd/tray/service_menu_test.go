package main

import (
	"fyne.io/systray"
	"github.com/somprasongd/go-thai-smartcard/pkg/ctl"
	"sync"
	"testing"
)

func TestConcurrentServiceMenuRefresh(t *testing.T) {
	tray := &tray{
		mAgent: &systray.MenuItem{}, mAgentStart: &systray.MenuItem{},
		mAgentStop: &systray.MenuItem{}, mAgentRestart: &systray.MenuItem{},
		serviceState: func() (ctl.State, error) { return ctl.StateRunning, nil },
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
