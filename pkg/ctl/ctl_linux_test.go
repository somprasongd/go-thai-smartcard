//go:build linux

package ctl

import (
	"errors"
	"testing"
)

func TestParseIsActive(t *testing.T) {
	boom := errors.New("no systemd bus")

	tests := []struct {
		name     string
		output   string
		exitCode int
		err      error
		want     State
		wantErr  bool
	}{
		{name: "active exits 0", output: "active\n", exitCode: 0, want: StateRunning},
		{name: "inactive exits 3 and still prints the state", output: "inactive\n", exitCode: 3, want: StateStopped},
		{name: "failed exits 3 and counts as stopped", output: "failed\n", exitCode: 3, want: StateStopped},
		{name: "an unknown state is unknown", output: "activating\n", exitCode: 0, want: StateUnknown, wantErr: true},
		{name: "no systemd at all is unknown", output: "", exitCode: -1, err: boom, want: StateUnknown, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseIsActive(tt.output, tt.exitCode, tt.err)
			if got != tt.want {
				t.Errorf("state = %v, want %v", got, tt.want)
			}
			if (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
