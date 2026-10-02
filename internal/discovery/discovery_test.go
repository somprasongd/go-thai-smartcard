package discovery

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLocalURL(t *testing.T) {
	for _, test := range []struct {
		address, want string
		bad           bool
	}{
		{"0.0.0.0:1234", "http://127.0.0.1:1234", false},
		{"[::]:1234", "http://[::1]:1234", false},
		{"[::1]:1234", "http://[::1]:1234", false},
		{"127.0.0.1:9898", "http://127.0.0.1:9898", false},
		{"192.168.1.5:9898", "", true},
		{"bad", "", true},
	} {
		t.Run(test.address, func(t *testing.T) {
			got, err := LocalURL(test.address)
			if (err != nil) != test.bad || got != test.want {
				t.Fatalf("LocalURL = %q, %v", got, err)
			}
		})
	}
}

func TestPublisherOwnershipGenerationAndCleanup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "endpoint.json")
	p, err := Open(path, "first", false)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if other, err := Open(path, "second", false); err == nil {
		other.Close()
		t.Fatal("second publisher acquired live slot")
	}
	for i := uint64(1); i <= 2; i++ {
		stage, err := p.Prepare("http://127.0.0.1:1234")
		if err != nil {
			t.Fatal(err)
		}
		if err = stage.Commit(); err != nil {
			t.Fatal(err)
		}
		stage.Abort()
		e, err := Read(path)
		if err != nil || e.Generation != i || e.InstanceID != "first" {
			t.Fatalf("endpoint = %#v, %v", e, err)
		}
	}
	if runtime.GOOS != "windows" {
		st, _ := os.Stat(path)
		if st.Mode().Perm() != 0600 {
			t.Fatalf("mode = %o", st.Mode().Perm())
		}
	}
	// Cleanup must not remove metadata now belonging to another instance.
	raw := []byte(`{"schema_version":1,"instance_id":"other","generation":1,"base_url":"http://127.0.0.1:1234"}`)
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	p.Close()
	if _, err = os.Stat(path); err != nil {
		t.Fatal("removed another instance's endpoint", err)
	}
	other, err := Open(path, "second", false)
	if err != nil {
		t.Fatal(err)
	}
	other.Close()
}

func TestPublicationFailureDoesNotAdvanceGeneration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "endpoint.json")
	p, err := Open(path, "instance", false)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	stage, err := p.Prepare(DefaultURL)
	if err != nil {
		t.Fatal(err)
	}
	defer stage.Abort()
	if err = os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err = stage.Commit(); err == nil {
		t.Fatal("replaced a directory with metadata")
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	next, err := p.Prepare(DefaultURL)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Abort()
	if next.Endpoint.Generation != 1 {
		t.Fatal("failed publication advanced generation")
	}
}

func TestReadRejectsUnsafeEndpoint(t *testing.T) {
	for _, u := range []string{"http://example.com:9898", "http://user:secret@127.0.0.1:9898", "http://127.0.0.1:9898?token=secret", "https://127.0.0.1:9898", "http://127.0.0.1:9898/api"} {
		e := Endpoint{1, "instance", 1, u}
		if e.Validate() == nil {
			t.Errorf("accepted %s", u)
		}
	}
}

func TestPublisherServicePermissionsAndOwnCleanup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "public", "endpoint.json")
	p, err := Open(path, "service", true)
	if err != nil {
		t.Fatal(err)
	}
	stage, err := p.Prepare(DefaultURL)
	if err != nil {
		t.Fatal(err)
	}
	defer stage.Abort()
	if err = stage.Commit(); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		file, _ := os.Stat(path)
		dir, _ := os.Stat(filepath.Dir(path))
		if file.Mode().Perm() != 0644 || dir.Mode().Perm() != 0755 {
			t.Fatalf("permissions %o/%o", file.Mode().Perm(), dir.Mode().Perm())
		}
	}
	p.Close()
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("own endpoint was not removed")
	}
}
