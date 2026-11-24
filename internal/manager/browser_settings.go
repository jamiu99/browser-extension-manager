package manager

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

type BrowserConfig struct {
	Executable    string `json:"executable"`
	DataDirectory string `json:"dataDirectory"`
	Profile       string `json:"profile"`
}

type BrowserStatus struct {
	ID               string        `json:"id"`
	Name             string        `json:"name"`
	SettingsFile     string        `json:"settingsFile"`
	Config           BrowserConfig `json:"config"`
	Detected         BrowserConfig `json:"detected"`
	Effective        BrowserConfig `json:"effective"`
	Profiles         []string      `json:"profiles"`
	Ready            bool          `json:"ready"`
	Problem          string        `json:"problem,omitempty"`
	AutomaticReload  bool          `json:"automaticReload"`
	defaultCustomDir bool
	page             string
}

type BrowserReport struct {
	Platform string          `json:"platform"`
	Browsers []BrowserStatus `json:"browsers"`
}

type BrowserSettings struct {
	Dir string
}

var browserList = []struct{ id, name, page string }{
	{"edge", "Edge", "edge://extensions"},
	{"chrome", "Chrome", "chrome://extensions"},
	{"chromium", "Chromium", "chrome://extensions"},
}

func executable(target string) string {
	if target == "" {
		return ""
	}
	if runtime.GOOS == "darwin" && strings.EqualFold(filepath.Ext(target), ".app") {
		name := strings.TrimSuffix(filepath.Base(target), filepath.Ext(target))
		target = filepath.Join(target, "Contents", "MacOS", name)
	}
	path, err := exec.LookPath(target)
	if err != nil || !filepath.IsAbs(path) {
		return ""
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || (runtime.GOOS == "windows" && !strings.EqualFold(filepath.Ext(path), ".exe")) {
		return ""
	}
	return filepath.Clean(path)
}

func (s BrowserSettings) settingsFile(id string) string {
	return filepath.Join(s.Dir, "browsers", id+".json")
}

func (s BrowserSettings) resolve(id string, requested *BrowserConfig) (BrowserStatus, error) {
	var status BrowserStatus
	for _, known := range browserList {
		if known.id == id {
			status.ID, status.Name, status.page = id, known.name, known.page
			break
		}
	}
	if status.ID == "" {
		return status, fmt.Errorf("请选择 edge、chrome 或 chromium")
	}
	status.SettingsFile = s.settingsFile(id)
	status.AutomaticReload = runtime.GOOS == "windows"
	status.Detected, status.defaultCustomDir = detectBrowser(id)
	if requested != nil {
		status.Config = *requested
	} else {
		data, err := os.ReadFile(status.SettingsFile)
		switch {
		case err == nil:
			data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
			decoder := json.NewDecoder(bytes.NewReader(data))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&status.Config); err != nil || decoder.Decode(new(any)) != io.EOF {
				status.Problem = "浏览器配置文件格式不正确，请重新保存设置。"
			}
		case !errors.Is(err, os.ErrNotExist):
			status.Problem = "无法读取浏览器配置文件。"
		}
	}
	// Keep the configured values visible so a moved installation can be repaired.
	status.Config.Executable = strings.TrimSpace(status.Config.Executable)
	status.Config.DataDirectory = strings.TrimSpace(status.Config.DataDirectory)
	status.Config.Profile = strings.TrimSpace(status.Config.Profile)
	status.Effective = status.Detected
	if status.Config.Executable != "" {
		status.Effective.Executable = ""
		if filepath.IsAbs(status.Config.Executable) {
			status.Effective.Executable = executable(status.Config.Executable)
		}
		if status.Effective.Executable == "" && status.Problem == "" {
			status.Problem = "请选择本机浏览器程序的有效绝对路径。"
		}
	}
	if status.Config.DataDirectory != "" {
		status.Effective.DataDirectory = filepath.Clean(status.Config.DataDirectory)
		info, err := os.Stat(status.Effective.DataDirectory)
		if (!filepath.IsAbs(status.Effective.DataDirectory) || err != nil || !info.IsDir()) && status.Problem == "" {
			status.Problem = "请选择已存在的浏览器用户数据目录。"
		}
	}
	status.Effective.Profile = status.Config.Profile
	status.Profiles = []string{}
	if status.Problem == "" {
		status.Profiles = profileNames(status.Effective.DataDirectory)
		if status.Config.Profile != "" && !slices.Contains(status.Profiles, status.Config.Profile) {
			status.Problem = "所选用户资料不存在，请先检查用户数据目录。"
		}
	}
	status.Ready = status.Problem == "" && status.Effective.Executable != ""
	return status, nil
}

func (s BrowserSettings) Inspect(id string, config BrowserConfig) (BrowserStatus, error) {
	return s.resolve(id, &config)
}

func (s BrowserSettings) Status(id string) (BrowserStatus, error) {
	return s.resolve(id, nil)
}

func (s BrowserSettings) Report() BrowserReport {
	report := BrowserReport{Platform: runtime.GOOS, Browsers: make([]BrowserStatus, 0, len(browserList))}
	for _, browser := range browserList {
		status, _ := s.resolve(browser.id, nil)
		report.Browsers = append(report.Browsers, status)
	}
	return report
}

func (s BrowserSettings) Available() []Browser {
	result := []Browser{}
	for _, status := range s.Report().Browsers {
		if status.Ready {
			result = append(result, Browser{
				ID: status.ID, Name: status.Name, Page: status.page, Exe: status.Effective.Executable,
				Profile: status.Effective.DataDirectory, Instance: status.Effective.Profile,
				UseDataDir: status.Config.DataDirectory != "" || status.defaultCustomDir,
			})
		}
	}
	return result
}

func (s BrowserSettings) Save(id string, config BrowserConfig) (BrowserStatus, error) {
	status, err := s.Inspect(id, config)
	if err != nil {
		return status, err
	}
	if status.Problem != "" {
		return status, errors.New(status.Problem)
	}
	if status.Config == (BrowserConfig{}) {
		if err := os.Remove(status.SettingsFile); err != nil && !errors.Is(err, os.ErrNotExist) {
			return status, err
		}
		return status, nil
	}
	return status, writeJSON(status.SettingsFile, status.Config)
}
