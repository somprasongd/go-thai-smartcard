package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"reflect"
	"sync"

	"github.com/somprasongd/go-thai-smartcard/internal/atomicfile"
	"github.com/somprasongd/go-thai-smartcard/internal/discovery"
	"github.com/somprasongd/go-thai-smartcard/pkg/config"
	"github.com/somprasongd/go-thai-smartcard/pkg/server"
	"github.com/somprasongd/go-thai-smartcard/pkg/smc"
)

// One coordinator belongs to the process, not to a particular listener's mux.
type settingsCoordinator struct {
	mu                sync.Mutex
	ready             chan struct{}
	path              string
	current           config.Config
	manager           *server.Manager
	publisher         *discovery.Publisher
	openPublisher     func() (*discovery.Publisher, error)
	serverConfig      func(config.Config) server.ServerConfig
	store             *smc.OptionsStore
	selection         *smc.ReaderStore
	commitPublication func(*discovery.Publication) error
}

func (c *settingsCoordinator) publishStartup() {
	p, err := c.openPublisher()
	if err != nil {
		log.Printf("WARNING: endpoint discovery unavailable; use tray --url: %v", err)
		return
	}
	c.publisher = p
	u, err := discovery.LocalURL(c.manager.PlainAddr())
	if err == nil {
		var publication *discovery.Publication
		publication, err = p.Prepare(u)
		if err == nil {
			defer publication.Abort()
			err = publication.Commit()
		}
	}
	if err != nil {
		log.Printf("WARNING: endpoint discovery unavailable; use tray --url: %v", err)
	}
}

func (c *settingsCoordinator) apply(next config.Config, expected string) (server.SettingsResult, error) {
	if c.ready != nil {
		<-c.ready
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	fail := func(err error) (server.SettingsResult, error) {
		return server.SettingsResult{}, &server.SettingsError{Status: http.StatusInternalServerError, Err: err}
	}
	if err := config.Validate(next); err != nil {
		return server.SettingsResult{}, &server.SettingsError{Status: http.StatusBadRequest, Err: err}
	}
	original, err := atomicfile.ReadFile(c.path)
	existed := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fail(err)
	}
	version := ""
	if existed {
		sum := sha256.Sum256(original)
		version = hex.EncodeToString(sum[:])
	}
	if version != expected {
		return server.SettingsResult{}, config.ErrStale
	}
	changed := !reflect.DeepEqual(next.Server, c.current.Server) || !reflect.DeepEqual(next.TLS, c.current.TLS)
	var prepared *server.Prepared
	url, urlErr := discovery.LocalURL(c.manager.PlainAddr())
	if changed {
		prepared, err = c.manager.Prepare(c.serverConfig(next))
		if err != nil {
			return fail(err)
		}
		defer func() {
			if prepared != nil {
				prepared.Abort()
			}
		}()
		url, urlErr = discovery.LocalURL(prepared.PlainAddr())
	}
	if c.publisher == nil && c.openPublisher != nil {
		c.publisher, err = c.openPublisher()
		if err != nil && changed {
			return fail(fmt.Errorf("cannot publish changed endpoint: %w", err))
		}
	}
	var publication *discovery.Publication
	if c.publisher != nil && urlErr == nil {
		publication, err = c.publisher.Prepare(url)
		if err != nil {
			return fail(fmt.Errorf("prepare endpoint: %w", err))
		}
		defer publication.Abort()
	} else if changed {
		if urlErr != nil {
			return fail(fmt.Errorf("cannot publish changed endpoint: %w", urlErr))
		}
		return fail(errors.New("cannot change endpoint without a discovery publisher"))
	}
	savedVersion, err := config.SaveVersion(c.path, next, expected)
	if err != nil {
		if errors.Is(err, config.ErrStale) {
			return server.SettingsResult{}, err
		}
		return fail(err)
	}
	if prepared != nil {
		prepared.Apply()
	}
	if publication != nil {
		commit := c.commitPublication
		if commit == nil {
			commit = func(p *discovery.Publication) error { return p.Commit() }
		}
		if err = commit(publication); err != nil {
			if prepared != nil {
				prepared.Rollback()
				prepared = nil
			}
			if rollbackErr := config.Restore(c.path, original, existed, savedVersion); rollbackErr != nil {
				err = fmt.Errorf("publish endpoint failed: %v; runtime kept previous values but config rollback failed: %w", err, rollbackErr)
				log.Printf("ERROR: %v", err)
			} else {
				err = fmt.Errorf("publish endpoint failed; previous settings restored: %w", err)
			}
			return fail(err)
		}
	}
	c.current = next
	if c.store != nil {
		c.store.Set(cardOptions(next.Card))
	}
	if c.selection != nil {
		c.selection.Set(next.Card.Reader)
	}
	result := server.SettingsResult{Version: savedVersion}
	if urlErr == nil {
		result.EndpointURL = url
	}
	if prepared != nil {
		result.AfterResponse = prepared.Retire
		prepared = nil
	}
	return result, nil
}
