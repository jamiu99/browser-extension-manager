package main

import (
	"fmt"

	"github.com/jamiu99/browser-extension-manager/internal/selfupdate"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type softwareVersion struct {
	Version string `json:"version"`
	Notice  string `json:"notice"`
}

func (a *App) SoftwareVersion() softwareVersion {
	return softwareVersion{Version: version, Notice: a.updateNotice}
}

func (a *App) CheckSoftwareUpdate() (selfupdate.Info, error) {
	return selfupdate.New().Check(a.ctx, version)
}

func (a *App) InstallSoftwareUpdate(selected string) error {
	if !a.updateMu.TryLock() {
		return fmt.Errorf("软件正在更新，请等待完成")
	}
	defer a.updateMu.Unlock()
	if err := selfupdate.New().Start(a.ctx, version, selected); err != nil {
		return err
	}
	runtime.Quit(a.ctx)
	return nil
}

func (a *App) OpenSoftwareReleases() {
	runtime.BrowserOpenURL(a.ctx, selfupdate.ReleasesURL)
}
