package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	for _, test := range []struct {
		left, right string
		want        int
		valid       bool
	}{
		{"v6.1.0", "6.0.9", 1, true},
		{"6.0.0", "v6.0.0", 0, true},
		{"5.9.99", "6.0.0", -1, true},
		{"dev", "6.0.0", 0, false},
		{"6.0", "6.0.0", 0, false},
		{"06.0.0", "6.0.0", 0, false},
	} {
		got, valid := compareVersions(test.left, test.right)
		if got != test.want || valid != test.valid {
			t.Errorf("compareVersions(%q, %q) = (%d, %t), want (%d, %t)", test.left, test.right, got, valid, test.want, test.valid)
		}
	}
}

func TestReleaseInstallerForPlatform(t *testing.T) {
	release := githubRelease{TagName: "v6.1.0", Assets: []releaseAsset{
		{Name: "thai-smartcard-setup-6.1.0.exe"},
		{Name: "thai-smartcard-agent-6.1.0.pkg"},
		{Name: "thai-smartcard-agent_6.1.0_amd64.deb"},
		{Name: "thai-smartcard-agent-6.1.0.x86_64.rpm"},
	}}
	for _, test := range []struct {
		goos, arch, packageType, want string
	}{
		{"windows", "amd64", "", "thai-smartcard-setup-6.1.0.exe"},
		{"darwin", "arm64", "", "thai-smartcard-agent-6.1.0.pkg"},
		{"darwin", "amd64", "", "thai-smartcard-agent-6.1.0.pkg"},
		{"linux", "amd64", "deb", "thai-smartcard-agent_6.1.0_amd64.deb"},
		{"linux", "amd64", "rpm", "thai-smartcard-agent-6.1.0.x86_64.rpm"},
	} {
		got, ok := release.installer(test.goos, test.arch, test.packageType)
		if !ok || got.Name != test.want {
			t.Errorf("installer(%q, %q, %q) = (%q, %t), want %q", test.goos, test.arch, test.packageType, got.Name, ok, test.want)
		}
	}
	if _, ok := release.installer("linux", "arm64", "deb"); ok {
		t.Fatal("found an installer for an unsupported architecture")
	}
	if _, ok := release.installer("linux", "amd64", ""); ok {
		t.Fatal("found an installer without a recognized package type")
	}
}

func TestPackageTypeForDistro(t *testing.T) {
	for _, test := range []struct {
		id, like, want string
	}{
		{"ubuntu", "debian", "deb"},
		{"linuxmint", "ubuntu debian", "deb"},
		{"fedora", "", "rpm"},
		{"rocky", "rhel", "rpm"},
		{"opensuse-tumbleweed", "suse", "rpm"},
		{"arch", "", ""},
	} {
		if got := packageTypeForDistro(test.id, test.like); got != test.want {
			t.Errorf("packageTypeForDistro(%q, %q) = %q, want %q", test.id, test.like, got, test.want)
		}
	}
}

func TestFetchLatestRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/invalid" {
			io.WriteString(w, `{"tag_name":"v6.1.0","html_url":"https://example.com/release"}`)
			return
		}
		if r.Header.Get("Accept") != "application/vnd.github+json" || r.Header.Get("User-Agent") == "" {
			t.Errorf("missing GitHub request headers: %v", r.Header)
		}
		io.WriteString(w, `{"tag_name":"v6.1.0","html_url":"https://github.com/somprasongd/go-thai-smartcard/releases/tag/v6.1.0"}`)
	}))
	defer server.Close()
	release, err := fetchLatestRelease(context.Background(), server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if release.TagName != "v6.1.0" {
		t.Fatalf("tag = %q, want v6.1.0", release.TagName)
	}

	if _, err := fetchLatestRelease(context.Background(), server.Client(), server.URL+"/invalid"); err == nil {
		t.Fatal("accepted a release page outside the project GitHub repository")
	}
}

func TestDownloadVerifiedInstaller(t *testing.T) {
	content := "synthetic installer bytes"
	digest := sha256.Sum256([]byte(content))
	asset := releaseAsset{
		Name:        "thai-smartcard-agent_6.1.0_amd64.deb",
		DownloadURL: "https://github.com/somprasongd/go-thai-smartcard/releases/download/v6.1.0/thai-smartcard-agent_6.1.0_amd64.deb",
		Digest:      "sha256:" + hex.EncodeToString(digest[:]),
		Size:        int64(len(content)),
	}
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet || req.URL.String() != asset.DownloadURL {
			t.Fatalf("unexpected download request: %s %s", req.Method, req.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(content)), Header: make(http.Header), Request: req}, nil
	})}
	cache := t.TempDir()
	path, err := downloadVerifiedInstaller(context.Background(), client, "v6.1.0", asset, cache)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != asset.Name {
		t.Fatalf("download path = %q", path)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != content {
		t.Fatalf("downloaded content = %q, %v", got, err)
	}
	if mode := fileMode(t, path); runtime.GOOS != "windows" && mode.Perm() != 0600 {
		t.Fatalf("installer cache mode = %o, want 600", mode.Perm())
	}

	asset.Digest = "sha256:" + strings.Repeat("0", sha256.Size*2)
	if _, err := downloadVerifiedInstaller(context.Background(), client, "v6.1.0", asset, t.TempDir()); err == nil {
		t.Fatal("accepted an installer with a mismatched digest")
	}
}

func TestInstallerOpenerReportsProcessFailure(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestInstallerOpenerHelperProcess$")
	cmd.Env = append(os.Environ(), "THAI_SMARTCARD_TEST_INSTALLER_HELPER=1")
	if err := runInstallerOpener(cmd); err == nil {
		t.Fatal("did not report an opener process that exited unsuccessfully")
	}
}

func TestInstallerOpenerHelperProcess(t *testing.T) {
	if os.Getenv("THAI_SMARTCARD_TEST_INSTALLER_HELPER") == "1" {
		os.Exit(17)
	}
}

func TestDownloadVerifiedInstallerRejectsUnsafeMetadata(t *testing.T) {
	goodDigest := "sha256:" + strings.Repeat("a", sha256.Size*2)
	for _, asset := range []releaseAsset{
		{Name: "../installer.exe", DownloadURL: "https://github.com/somprasongd/go-thai-smartcard/releases/download/v1.0.0/installer.exe", Digest: goodDigest},
		{Name: "installer.exe", DownloadURL: "https://example.com/installer.exe", Digest: goodDigest},
		{Name: "installer.exe", DownloadURL: "https://github.com/somprasongd/go-thai-smartcard/releases/download/v1.0.0/installer.exe", Digest: "sha512:bad"},
	} {
		if _, err := downloadVerifiedInstaller(context.Background(), http.DefaultClient, "v1.0.0", asset, t.TempDir()); err == nil {
			t.Fatalf("accepted unsafe asset metadata: %+v", asset)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode()
}
