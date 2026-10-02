package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/somprasongd/go-thai-smartcard/internal/atomicfile"
	"path/filepath"
	"sync"
	"testing"
)

func TestLoadVersionMatchesSnapshotDuringReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	snapshots := map[string]int{}
	raws := make([][]byte, 2)
	for i := range raws {
		raws[i] = []byte(fmt.Sprintf("[server]\nport = %d\n", 9900+i))
		sum := sha256.Sum256(raws[i])
		snapshots[hex.EncodeToString(sum[:])] = 9900 + i
	}
	if err := atomicfile.Write(path, raws[0], 0600); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := 0; i < 200; i++ {
			if err := atomicfile.Write(path, raws[i%2], 0600); err != nil {
				t.Error(err)
				return
			}
		}
	})
	for i := 0; i < 500; i++ {
		cfg, version, err := LoadVersion(path)
		if err != nil {
			t.Fatal(err)
		}
		if snapshots[version] != cfg.Server.Port {
			t.Fatal("config/version came from different snapshots")
		}
	}
	wg.Wait()
}
