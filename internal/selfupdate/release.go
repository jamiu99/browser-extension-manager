package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const Repository = "jamiu99/browser-extension-manager"
const ReleasesURL = "https://github.com/" + Repository + "/releases"
const latestURL = "https://api.github.com/repos/" + Repository + "/releases/latest"
const checksumName = "SHA256SUMS.txt"
const archiveLimit = 128 << 20

var versionPattern = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z][0-9A-Za-z.-]*)?$`)

type Info struct {
	Current string `json:"current"`
	Latest  string `json:"latest"`
	State   string `json:"state"`
	Size    int64  `json:"size"`
	asset   asset
}

type asset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	State  string `json:"state"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
}

type Service struct {
	client *http.Client
}

func New() Service {
	return Service{&http.Client{
		Timeout: 5 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 || !githubURL(req.URL) {
				return fmt.Errorf("GitHub 返回了无效的下载地址")
			}
			return nil
		},
	}}
}

func githubURL(u *url.URL) bool {
	return u.Scheme == "https" && u.User == nil && u.Port() == "" &&
		(u.Host == "api.github.com" || u.Host == "github.com" ||
			u.Host == "release-assets.githubusercontent.com" || u.Host == "objects.githubusercontent.com")
}

func compare(current, latest string) string {
	next := versionPattern.FindStringSubmatch(latest)
	if len(next) == 0 || next[4] != "" {
		return "invalid"
	}
	old := versionPattern.FindStringSubmatch(current)
	if len(old) == 0 {
		return "development"
	}
	for i := 1; i <= 3; i++ {
		// Compare the canonical decimal strings without overflowing.
		if len(old[i]) != len(next[i]) {
			if len(old[i]) < len(next[i]) {
				return "available"
			}
			return "ahead"
		}
		if old[i] < next[i] {
			return "available"
		}
		if old[i] > next[i] {
			return "ahead"
		}
	}
	if old[4] != "" {
		return "available"
	}
	return "current"
}

func assetName(tag, platform, arch string) string {
	suffix := ""
	switch {
	case platform == "windows" && arch == "amd64":
		suffix = "windows-x64.zip"
	case platform == "darwin" && (arch == "arm64" || arch == "amd64"):
		suffix = "macos-universal.zip"
	case platform == "linux" && arch == "amd64":
		suffix = "linux-x64.tar.gz"
	}
	if suffix == "" {
		return ""
	}
	return "extension-manager-" + tag + "-" + suffix
}

func (s Service) get(ctx context.Context, address string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil || !githubURL(req.URL) {
		return nil, fmt.Errorf("GitHub 更新地址无效")
	}
	req.Header.Set("User-Agent", "browser-extension-manager")
	if address == latestURL {
		req.Header.Set("Accept", "application/vnd.github+json")
	}
	res, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("连接 GitHub 失败，请检查网络后重试")
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		if res.StatusCode == http.StatusForbidden || res.StatusCode == http.StatusTooManyRequests {
			return nil, fmt.Errorf("GitHub 暂时限制了请求，请稍后再试")
		}
		return nil, fmt.Errorf("GitHub 暂时无法提供更新，请稍后重试")
	}
	return res, nil
}

func downloadURL(tag, name string) string {
	return ReleasesURL + "/download/" + tag + "/" + name
}

func (s Service) Check(ctx context.Context, current string) (Info, error) {
	return s.check(ctx, current, runtime.GOOS, runtime.GOARCH)
}

func (s Service) check(ctx context.Context, current, platform, arch string) (Info, error) {
	result := Info{Current: current}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	res, err := s.get(ctx, latestURL)
	if err != nil {
		return result, err
	}
	defer res.Body.Close()
	var release struct {
		Tag        string  `json:"tag_name"`
		Draft      bool    `json:"draft"`
		Prerelease bool    `json:"prerelease"`
		Assets     []asset `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(&release); err != nil {
		return result, fmt.Errorf("无法读取 GitHub 发布信息，请稍后重试")
	}
	result.Latest = release.Tag
	result.State = compare(current, release.Tag)
	if release.Draft || release.Prerelease || result.State == "invalid" {
		return result, fmt.Errorf("暂时没有可用的正式版本")
	}
	if result.State != "available" {
		return result, nil
	}
	name := assetName(release.Tag, platform, arch)
	hasChecksum := false
	for _, item := range release.Assets {
		if item.State != "uploaded" || item.URL != downloadURL(release.Tag, item.Name) {
			continue
		}
		switch item.Name {
		case checksumName:
			hasChecksum = item.Size > 0 && item.Size <= 64<<10
		case name:
			if name != "" && item.Size > 0 && item.Size <= archiveLimit {
				result.asset, result.Size = item, item.Size
			}
		}
	}
	if result.Size == 0 || !hasChecksum {
		result.State = "manual"
	}
	return result, nil
}

func checksum(data, name string) (string, error) {
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			digest, err := hex.DecodeString(fields[0])
			if err == nil && len(digest) == sha256.Size {
				return strings.ToLower(fields[0]), nil
			}
		}
	}
	return "", fmt.Errorf("发布包缺少有效的 SHA-256 校验值")
}

func (s Service) download(ctx context.Context, update Info, destination string) error {
	res, err := s.get(ctx, downloadURL(update.Latest, checksumName))
	if err != nil {
		return err
	}
	data, readErr := io.ReadAll(io.LimitReader(res.Body, (64<<10)+1))
	res.Body.Close()
	if readErr != nil || len(data) > 64<<10 {
		return fmt.Errorf("无法读取发布包的校验文件")
	}
	expected, err := checksum(string(data), update.asset.Name)
	if err != nil {
		return err
	}
	if update.asset.Digest != "" && update.asset.Digest != "sha256:"+expected {
		return fmt.Errorf("GitHub 发布记录与校验文件不一致，请稍后重试")
	}
	res, err = s.get(ctx, update.asset.URL)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(res.Body, update.asset.Size+1))
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		return fmt.Errorf("下载更新未完成，请重试")
	}
	if size != update.asset.Size || hex.EncodeToString(hash.Sum(nil)) != expected {
		return fmt.Errorf("下载包校验失败，请重新下载")
	}
	return nil
}
