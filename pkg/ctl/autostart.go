package ctl

// EnsureRunning starts only a confirmed stopped service. Unknown states may
// represent startup in progress, so automatic callers must leave them alone.
// macOS uses the helper only: login must never open an administrator prompt.
func EnsureRunning(m Manager) error {
	state, err := m.State()
	if err != nil || state != StateStopped {
		return err
	}
	if auto, ok := m.(interface{ startAutomatic() error }); ok {
		err = auto.startAutomatic()
	} else {
		err = m.Start()
	}
	if err != nil {
		// Another login or the boot manager may have won after our query.
		if state, queryErr := m.State(); queryErr == nil && state == StateRunning {
			return nil
		}
	}
	return err
}
