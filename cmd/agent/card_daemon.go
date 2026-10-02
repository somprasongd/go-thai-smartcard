package main

import (
	"context"
	"log"
	"time"

	"github.com/somprasongd/go-thai-smartcard/pkg/model"
	"github.com/somprasongd/go-thai-smartcard/pkg/smc"
	"github.com/somprasongd/go-thai-smartcard/pkg/transport"
)

const cardRetryInterval = 2 * time.Second

// runCardDaemon owns each transport until the daemon has released its session.
// A new context after failure also recovers a PC/SC broker restarted underneath us.
func runCardDaemon(ctx context.Context, cfg smc.DaemonConfig, open func() (transport.Transport, error), retry time.Duration) {
	for ctx.Err() == nil {
		backend, err := open()
		if err == nil {
			err = smc.NewSmartCardWith(backend).StartDaemonWith(ctx, cfg)
		}
		if backend != nil {
			if closeErr := backend.Close(); closeErr != nil {
				log.Printf("close card transport: %v", closeErr)
			}
		}
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Printf("card daemon: %v; retrying in %s", err, retry)
			if cfg.Broadcast != nil {
				select {
				case cfg.Broadcast <- model.Message{Event: "smc-error", Payload: map[string]string{"message": err.Error()}}:
				case <-ctx.Done():
					return
				}
			}
		}
		timer := time.NewTimer(retry)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
