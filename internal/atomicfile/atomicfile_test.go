package atomicfile

import (
	"bytes"
	"path/filepath"
	"sync"
	"testing"
)

func TestReadersNeverSeePartialReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	first := bytes.Repeat([]byte("a"), 16384)
	second := bytes.Repeat([]byte("b"), 32768)
	if err := Write(path, first, 0600); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	fail := make(chan error, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			raw, err := ReadFile(path)
			if err != nil {
				select {
				case fail <- err:
				default:
				}
				return
			}
			if !bytes.Equal(raw, first) && !bytes.Equal(raw, second) {
				t.Error("reader saw partial replacement")
				return
			}
		}
	}()
	for range 30 {
		if err := Write(path, second, 0600); err != nil {
			t.Fatal(err)
		}
		if err := Write(path, first, 0600); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()
	select {
	case err := <-fail:
		t.Fatal(err)
	default:
	}
}
