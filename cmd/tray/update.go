package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	latestReleaseURL = "https://api.github.com/repos/somprasongd/go-thai-smartcard/releases/latest"
	maxReleaseJSON   = 2 << 20
	maxInstallerSize = 128 << 20
)

type githubRelease struct {
	TagName string         `json:"tag_name"`
	HTMLURL string         `json:"html_url"`
	Assets  []releaseAsset `json:"assets"`
}

type releaseAsset struct {
	Name        string `json:"name"`
	DownloadURL string `json:"browser_download_url"`
	Digest      string `json:"digest"`
	Size        int64  `json:"size"`
}

type updateTarget struct {
	release githubRelease
	asset   *releaseAsset
}

func (t *tray) checkForUpdate(ctx context.Context) {
	t.mUpdate.SetTitle(t.lang.text("กำลังตรวจสอบอัปเดต…", "Checking for updates…"))
	t.mUpdate.Disable()
	version, err := readAgentVersion(ctx, t.baseURL(), &http.Client{Timeout: 5 * time.Second})
	if err != nil {
		log.Printf("check agent version: %v", err)
		t.finishUpdateCheck(nil, t.lang.text("ตรวจอัปเดตไม่ได้ — คลิกเพื่อลองอีกครั้ง", "Update check failed — click to retry"), true)
		return
	}
	client := &http.Client{Timeout: 10 * time.Second}
	release, err := fetchLatestRelease(ctx, client, latestReleaseURL)
	if err != nil {
		log.Printf("check latest GitHub release: %v", err)
		t.finishUpdateCheck(nil, t.lang.text("ตรวจอัปเดตไม่ได้ — คลิกเพื่อลองอีกครั้ง", "Update check failed — click to retry"), true)
		return
	}
	comparison, ok := compareVersions(release.TagName, version)
	if !ok {
		log.Printf("cannot compare agent version %q with release %q", version, release.TagName)
		t.finishUpdateCheck(nil, t.lang.text("ตรวจอัปเดตไม่ได้ — คลิกเพื่อลองอีกครั้ง", "Update check failed — click to retry"), true)
		return
	}
	if comparison <= 0 {
		t.finishUpdateCheck(nil, t.lang.text("agent เป็นเวอร์ชันล่าสุดแล้ว", "Agent is up to date"), false)
		return
	}

	asset, found := release.installer(runtime.GOOS, runtime.GOARCH, linuxPackageType())
	target := &updateTarget{release: release}
	if found {
		target.asset = &asset
	}
	t.finishUpdateCheck(target, releaseMenuTitle(t.lang, release.TagName, found), true)
	message := t.lang.text(
		"พบ agent รุ่น "+release.TagName+" เลือกรายการอัปเดตใน tray เพื่อดาวน์โหลดและเปิด installer ระบบจะให้ยืนยันก่อนติดตั้ง",
		"Agent "+release.TagName+" is available. Select the update item in the tray to download and open the installer. The operating system will ask before installing.",
	)
	alert(t.lang, t.lang.text("มีอัปเดต Thai Smartcard", "Thai Smartcard update available"), message)
}

func (t *tray) finishUpdateCheck(target *updateTarget, title string, enabled bool) {
	t.updateMu.Lock()
	t.updateTarget = target
	t.updateMu.Unlock()
	t.mUpdate.SetTitle(title)
	if target != nil {
		t.mUpdate.SetTooltip(t.lang.text("ดาวน์โหลดและเปิด installer สำหรับรุ่นนี้", "Download and open this release's installer"))
	} else {
		t.mUpdate.SetTooltip(title)
	}
	if enabled {
		t.mUpdate.Enable()
	} else {
		t.mUpdate.Disable()
	}
}

func (t *tray) openUpdate(ctx context.Context) {
	if !t.updateOps.TryLock() {
		return
	}
	defer t.updateOps.Unlock()
	t.updateMu.RLock()
	target := t.updateTarget
	t.updateMu.RUnlock()
	if target == nil {
		go t.checkForUpdate(ctx)
		return
	}
	if target.asset == nil {
		openBrowser(target.release.HTMLURL)
		return
	}

	t.mUpdate.SetTitle(t.lang.text("กำลังดาวน์โหลด installer…", "Downloading installer…"))
	cache, err := os.UserCacheDir()
	if err == nil {
		cache = filepath.Join(cache, "thai-smartcard", "updates", strings.TrimPrefix(target.release.TagName, "v"))
	}
	var installer string
	if err == nil {
		installer, err = downloadVerifiedInstaller(ctx, newReleaseHTTPClient(), target.release.TagName, *target.asset, cache)
	}
	if err == nil {
		err = openInstaller(installer)
	}
	if err != nil {
		log.Printf("open %s installer: %v", target.release.TagName, err)
		t.mUpdate.SetTitle(releaseMenuTitle(t.lang, target.release.TagName, true))
		openBrowser(target.release.HTMLURL)
		alert(t.lang, t.lang.text("เปิด installer ไม่สำเร็จ", "Could not open installer"), err.Error()+"\n"+target.release.HTMLURL)
		return
	}
	t.mUpdate.SetTitle(t.lang.text("เปิด installer แล้ว", "Installer opened"))
	t.mUpdate.Disable()
}

func releaseMenuTitle(l language, version string, hasInstaller bool) string {
	if hasInstaller {
		return l.text("ติดตั้งอัปเดต "+version+"…", "Install update "+version+"…")
	}
	return l.text("ดูอัปเดต "+version+"…", "View update "+version+"…")
}

func readAgentVersion(ctx context.Context, baseURL string, client *http.Client) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/api/info", nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("agent info: HTTP %d", resp.StatusCode)
	}
	var info struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&info); err != nil {
		return "", err
	}
	if _, ok := parseVersion(info.Version); !ok {
		return "", fmt.Errorf("agent returned an unsupported version %q", info.Version)
	}
	return info.Version, nil
}

func fetchLatestRelease(ctx context.Context, client *http.Client, endpoint string) (githubRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return githubRelease{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "thai-smartcard-tray")
	resp, err := client.Do(req)
	if err != nil {
		return githubRelease{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return githubRelease{}, fmt.Errorf("GitHub Releases: HTTP %d", resp.StatusCode)
	}
	var release githubRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxReleaseJSON)).Decode(&release); err != nil {
		return githubRelease{}, err
	}
	if _, ok := parseVersion(release.TagName); !ok {
		return githubRelease{}, fmt.Errorf("unsupported release tag %q", release.TagName)
	}
	if !validReleasePage(release.HTMLURL, release.TagName) {
		return githubRelease{}, errors.New("GitHub returned an invalid release page URL")
	}
	return release, nil
}

func newReleaseHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 2 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "https" || !releaseDownloadHost(req.URL.Hostname()) {
				return errors.New("release download redirected to an untrusted host")
			}
			return nil
		},
	}
}

func releaseDownloadHost(host string) bool {
	switch strings.ToLower(host) {
	case "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com":
		return true
	default:
		return false
	}
}

func (r githubRelease) installer(goos, goarch, packageType string) (releaseAsset, bool) {
	_, ok := parseVersion(r.TagName)
	if !ok || goarch != "amd64" && !(goos == "darwin" && goarch == "arm64") {
		return releaseAsset{}, false
	}
	versionText := strings.TrimPrefix(r.TagName, "v")
	name := ""
	switch goos {
	case "windows":
		name = "thai-smartcard-setup-" + versionText + ".exe"
	case "darwin":
		name = "thai-smartcard-agent-" + versionText + ".pkg"
	case "linux":
		switch packageType {
		case "deb":
			name = "thai-smartcard-agent_" + versionText + "_amd64.deb"
		case "rpm":
			name = "thai-smartcard-agent-" + versionText + ".x86_64.rpm"
		}
	}
	if name == "" {
		return releaseAsset{}, false
	}
	for _, asset := range r.Assets {
		if asset.Name == name {
			return asset, true
		}
	}
	return releaseAsset{}, false
}

func compareVersions(a, b string) (int, bool) {
	left, okLeft := parseVersion(a)
	right, okRight := parseVersion(b)
	if !okLeft || !okRight {
		return 0, false
	}
	for i := range left {
		if left[i] < right[i] {
			return -1, true
		}
		if left[i] > right[i] {
			return 1, true
		}
	}
	return 0, true
}

func parseVersion(version string) ([3]uint64, bool) {
	var parsed [3]uint64
	version = strings.TrimPrefix(version, "v")
	parts := strings.Split(version, ".")
	if len(parts) != len(parsed) {
		return parsed, false
	}
	for i, part := range parts {
		if part == "" || len(part) > 1 && part[0] == '0' {
			return parsed, false
		}
		value, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return parsed, false
		}
		parsed[i] = value
	}
	return parsed, true
}

func linuxPackageType() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok || key != "ID" && key != "ID_LIKE" {
			continue
		}
		values[key] = strings.Trim(strings.TrimSpace(value), "\"'")
	}
	return packageTypeForDistro(values["ID"], values["ID_LIKE"])
}

func packageTypeForDistro(id, idLike string) string {
	for _, distro := range strings.Fields(strings.ToLower(id + " " + idLike)) {
		switch distro {
		case "debian", "ubuntu", "linuxmint", "pop", "raspbian", "kali":
			return "deb"
		case "fedora", "rhel", "centos", "rocky", "almalinux", "ol", "amzn", "suse", "opensuse", "opensuse-leap", "opensuse-tumbleweed":
			return "rpm"
		}
	}
	return ""
}

func validReleasePage(raw, tag string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host == "github.com" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && u.Path == "/somprasongd/go-thai-smartcard/releases/tag/"+tag
}

func validAssetURL(raw, tag, name string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host == "github.com" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && u.Path == "/somprasongd/go-thai-smartcard/releases/download/"+tag+"/"+name
}

func downloadVerifiedInstaller(ctx context.Context, client *http.Client, tag string, asset releaseAsset, cacheDir string) (string, error) {
	if asset.Name == "" || filepath.Base(asset.Name) != asset.Name {
		return "", errors.New("invalid release asset name")
	}
	if _, ok := parseVersion(tag); !ok || !validAssetURL(asset.DownloadURL, tag, asset.Name) {
		return "", errors.New("invalid release asset URL")
	}
	expected, err := parseSHA256Digest(asset.Digest)
	if err != nil {
		return "", err
	}
	if asset.Size < 0 || asset.Size > maxInstallerSize {
		return "", errors.New("installer size is outside the allowed limit")
	}
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		return "", err
	}
	destination := filepath.Join(cacheDir, asset.Name)
	if got, err := fileSHA256(destination); err == nil && got == expected {
		if info, statErr := os.Stat(destination); statErr == nil && (asset.Size == 0 || info.Size() == asset.Size) {
			return destination, nil
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.DownloadURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("installer download: HTTP %d", resp.StatusCode)
	}
	file, err := os.CreateTemp(cacheDir, ".installer-*.part")
	if err != nil {
		return "", err
	}
	tempPath := file.Name()
	defer os.Remove(tempPath)
	hash := sha256.New()
	count, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(resp.Body, maxInstallerSize+1))
	if copyErr == nil && count > maxInstallerSize {
		copyErr = errors.New("installer exceeds the allowed size")
	}
	if copyErr == nil && asset.Size > 0 && count != asset.Size {
		copyErr = fmt.Errorf("installer size mismatch: got %d bytes, expected %d", count, asset.Size)
	}
	if copyErr == nil && hex.EncodeToString(hash.Sum(nil)) != expected {
		copyErr = errors.New("installer SHA-256 does not match GitHub's release digest")
	}
	if copyErr == nil {
		copyErr = file.Sync()
	}
	if closeErr := file.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		return "", copyErr
	}
	if err := os.Chmod(tempPath, 0600); err != nil {
		return "", err
	}
	if err := os.Remove(destination); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := os.Rename(tempPath, destination); err != nil {
		return "", err
	}
	return destination, nil
}

func parseSHA256Digest(digest string) (string, error) {
	algorithm, value, ok := strings.Cut(digest, ":")
	if !ok || algorithm != "sha256" || len(value) != sha256.Size*2 {
		return "", errors.New("release asset has no valid SHA-256 digest")
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", errors.New("release asset has an invalid SHA-256 digest")
	}
	return strings.ToLower(value), nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if info.Size() > maxInstallerSize {
		return "", errors.New("cached installer exceeds the allowed size")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, io.LimitReader(file, maxInstallerSize+1)); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
