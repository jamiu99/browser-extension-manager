package selfupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const stagePrefix = ".extension-manager-update-"
const finishFlag = "--finish-software-update"
const resultFlag = "--software-update-result"

type installation struct {
	exe, target, parent, stagedApp, stagedExe, cli string
}

type updateJob struct {
	Exe     string
	Current string
	Version string
	PID     int
}

type updateResult struct {
	Message string
}

type replacement struct {
	from, to string
}

func installLayout(exe, platform string) (installation, error) {
	exe, err := filepath.EvalSymlinks(exe)
	if err != nil || !filepath.IsAbs(exe) {
		return installation{}, fmt.Errorf("无法定位当前安装目录，请从 GitHub 下载新版")
	}
	result := installation{exe: exe, target: exe, stagedExe: "extension-manager", cli: "extension-manager-cli"}
	switch platform {
	case "windows":
		result.stagedExe += ".exe"
		result.cli += ".exe"
	case "darwin":
		macos := filepath.Dir(exe)
		contents := filepath.Dir(macos)
		bundle := filepath.Dir(contents)
		if filepath.Base(macos) != "MacOS" || filepath.Base(contents) != "Contents" ||
			!strings.HasSuffix(filepath.Base(bundle), ".app") || filepath.Base(exe) != "extension-manager" {
			return installation{}, fmt.Errorf("请将应用放在可写的应用目录中，或从 GitHub 下载新版")
		}
		result.target = bundle
		result.stagedApp = appBundle
		result.stagedExe = filepath.Join(appBundle, "Contents", "MacOS", "extension-manager")
	}
	if result.stagedApp == "" {
		result.stagedApp = result.stagedExe
	}
	result.parent = filepath.Dir(result.target)
	return result, nil
}

func probe(exe string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "--version")
	detachConsole(cmd)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return nil, fmt.Errorf("missing executable version")
	}
	return fields, nil
}

func copyExecutable(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0700)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	return errors.Join(err, out.Close())
}

func jsonFile(path string, value any) error {
	contents, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return os.WriteFile(path, contents, 0600)
}

func workerName() string {
	if runtime.GOOS == "windows" {
		return "update-worker.exe"
	}
	return "update-worker"
}

// Start downloads and verifies a stable release before handing off to a short-lived worker.
func (s Service) Start(ctx context.Context, current, selected string) error {
	info, err := s.Check(ctx, current)
	if err != nil {
		return err
	}
	if info.State != "available" || info.Latest != selected {
		return fmt.Errorf("发布版本已变化，请重新检查更新")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	layout, err := installLayout(exe, runtime.GOOS)
	if err != nil {
		return err
	}
	stage, err := os.MkdirTemp(layout.parent, stagePrefix)
	if err != nil {
		return fmt.Errorf("当前安装目录无法写入，请从 GitHub 下载新版")
	}
	handedOff := false
	defer func() {
		if !handedOff {
			os.RemoveAll(stage)
		}
	}()
	archive := filepath.Join(stage, info.asset.Name)
	if err := s.download(ctx, info, archive); err != nil {
		return err
	}
	payload := filepath.Join(stage, "payload")
	if err := unpack(archive, payload, strings.HasSuffix(info.asset.Name, ".zip")); err != nil {
		return fmt.Errorf("无法解压更新包，请重新下载")
	}
	metadata, err := payloadMetadata(payload, info.Latest)
	if err != nil {
		return err
	}
	newExe := filepath.Join(payload, layout.stagedExe)
	if err := os.Chmod(newExe, 0755); err != nil {
		return fmt.Errorf("更新包缺少应用程序")
	}
	fields, err := probe(newExe)
	if err != nil || len(fields) != 3 || fields[0] != metadata.Version || fields[1] != metadata.Commit || fields[2] != metadata.BuiltAt {
		return fmt.Errorf("此更新包无法在当前系统运行")
	}
	worker := filepath.Join(stage, workerName())
	// The extracted new application is signed on macOS. The updater worker
	// runs outside the installed bundle so the bundle can be replaced.
	if err := copyExecutable(layout.exe, worker); err != nil {
		return fmt.Errorf("无法准备更新程序，请重试")
	}
	job := updateJob{layout.exe, current, info.Latest, os.Getpid()}
	if err := jsonFile(filepath.Join(stage, "job.json"), job); err != nil {
		return err
	}
	cmd := exec.Command(worker, finishFlag)
	cmd.Dir = stage
	detachConsole(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("无法启动更新程序，请重试")
	}
	_ = cmd.Process.Release()
	handedOff = true
	return nil
}

func readJob(stage string) (updateJob, installation, error) {
	var job updateJob
	if !filepath.IsAbs(stage) || !strings.HasPrefix(filepath.Base(stage), stagePrefix) {
		return job, installation{}, fmt.Errorf("invalid update workspace")
	}
	info, err := os.Lstat(stage)
	if err != nil || !info.IsDir() {
		return job, installation{}, fmt.Errorf("missing update workspace")
	}
	b, err := os.ReadFile(filepath.Join(stage, "job.json"))
	if err != nil || json.Unmarshal(b, &job) != nil || job.PID <= 0 || compare(job.Current, job.Version) != "available" {
		return job, installation{}, fmt.Errorf("invalid update instructions")
	}
	layout, err := installLayout(job.Exe, runtime.GOOS)
	if err != nil || layout.exe != job.Exe || filepath.Dir(stage) != layout.parent {
		return job, installation{}, fmt.Errorf("invalid update target")
	}
	return job, layout, nil
}

func replacements(stage string, layout installation) ([]replacement, error) {
	root := filepath.Join(stage, "payload")
	result := []replacement{{from: filepath.Join(root, layout.stagedApp), to: layout.target}}
	cli := filepath.Join(root, layout.cli)
	if info, err := os.Lstat(cli); err == nil {
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("invalid command-line tool")
		}
		if err := os.Chmod(cli, 0755); err != nil {
			return nil, err
		}
		result = append(result, replacement{from: cli, to: filepath.Join(layout.parent, layout.cli)})
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return result, nil
}

// swap rolls back earlier replacements if a file move or the restart fails.
func swap(stage string, files []replacement, restart func() error) (error, bool) {
	type move struct {
		replacement
		backup    string
		installed bool
	}
	var moves []move
	rollback := func(failure error) (error, bool) {
		var undo error
		for i := len(moves) - 1; i >= 0; i-- {
			file := moves[i]
			if file.installed {
				undo = errors.Join(undo, os.Rename(file.to, file.from))
			}
			if file.backup != "" {
				undo = errors.Join(undo, os.Rename(file.backup, file.to))
			}
		}
		return errors.Join(failure, undo), undo != nil
	}
	for i, file := range files {
		entry := move{replacement: file}
		if info, err := os.Lstat(file.to); err == nil {
			if (i > 0 && !info.Mode().IsRegular()) || info.Mode()&os.ModeSymlink != 0 {
				return rollback(fmt.Errorf("invalid installation target"))
			}
			entry.backup = filepath.Join(stage, fmt.Sprintf("previous-%d", i))
			if err := os.Rename(file.to, entry.backup); err != nil {
				return rollback(err)
			}
		} else if !os.IsNotExist(err) {
			return rollback(err)
		}
		moves = append(moves, entry)
		if err := os.Rename(file.from, file.to); err != nil {
			return rollback(err)
		}
		moves[len(moves)-1].installed = true
	}
	if err := restart(); err != nil {
		return rollback(err)
	}
	return nil, false
}

func restart(layout installation, stage string, result updateResult, waitUntilReady bool) error {
	if err := jsonFile(filepath.Join(stage, "result.json"), result); err != nil {
		return err
	}
	cmd := exec.Command(layout.exe, resultFlag, stage)
	cmd.Dir = layout.parent
	detachConsole(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	if !waitUntilReady {
		_ = cmd.Process.Release()
		return nil
	}
	finished := make(chan error, 1)
	go func() { finished <- cmd.Wait() }()
	timeout := time.NewTimer(60 * time.Second)
	defer timeout.Stop()
	poll := time.NewTicker(100 * time.Millisecond)
	defer poll.Stop()
	for {
		select {
		case err := <-finished:
			return errors.Join(fmt.Errorf("new application exited before the window opened"), err)
		case <-timeout.C:
			if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				return err
			}
			select {
			case <-finished:
			case <-time.After(5 * time.Second):
			}
			return fmt.Errorf("new application did not open a window")
		case <-poll.C:
			if _, err := os.Stat(filepath.Join(stage, "ready")); err == nil {
				return nil
			}
		}
	}
}

// CompleteWorker is invoked by a verified copy of the original application after staging.
func CompleteWorker() error {
	exe, err := os.Executable()
	if err != nil || filepath.Base(exe) != workerName() {
		return fmt.Errorf("invalid update worker")
	}
	stage := filepath.Dir(exe)
	job, layout, err := readJob(stage)
	if err != nil {
		return err
	}
	if err := waitForExit(job.PID, 45*time.Second); err != nil {
		return err
	}
	unlock, err := lockTarget(filepath.Join(layout.parent, ".extension-manager-update.lock"))
	if err != nil {
		return err
	}
	defer unlock()
	fields, err := probe(layout.exe)
	if err != nil || fields[0] != job.Current {
		os.RemoveAll(filepath.Join(stage, "payload"))
		return fmt.Errorf("installation changed while preparing the update")
	}
	files, err := replacements(stage, layout)
	keep := false
	if err == nil {
		err, keep = swap(stage, files, func() error {
			return restart(layout, stage, updateResult{Message: "软件已更新到 " + job.Version + "。"}, true)
		})
	}
	if err == nil {
		_ = os.WriteFile(filepath.Join(stage, "cleanup"), nil, 0600)
		return nil
	}
	message := "软件更新未完成，已保留原版本。可以重新检查更新后重试。"
	if keep {
		message = "软件更新未完成，原文件保存在 " + stage
	}
	restartErr := restart(layout, stage, updateResult{Message: message}, false)
	if restartErr == nil && !keep {
		_ = os.WriteFile(filepath.Join(stage, "cleanup"), nil, 0600)
	}
	return errors.Join(err, restartErr)
}

// HandleArguments consumes the private worker and restart flags.
func HandleArguments(args []string) (notice string, ranWorker bool, err error) {
	if len(args) == 1 && args[0] == finishFlag {
		return "", true, CompleteWorker()
	}
	if len(args) != 2 || args[0] != resultFlag {
		return "", false, nil
	}
	stage := args[1]
	job, _, err := readJob(stage)
	if err != nil {
		return "", false, err
	}
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil || job.Exe != exe {
		return "", false, fmt.Errorf("invalid restart target")
	}
	b, err := os.ReadFile(filepath.Join(stage, "result.json"))
	var result updateResult
	if err != nil || json.Unmarshal(b, &result) != nil {
		return "", false, fmt.Errorf("invalid update result")
	}
	return result.Message, false, nil
}

// MarkReady is called once the relaunched application's WebView has loaded.
// The worker retains the old files until this point.
func MarkReady(stage string) {
	if stage == "" || os.WriteFile(filepath.Join(stage, "ready"), nil, 0600) != nil {
		return
	}
	go func() {
		// On Windows the worker executable is locked until the worker exits.
		approved := false
		for i := 0; i < 20; i++ {
			if !approved {
				_, err := os.Stat(filepath.Join(stage, "cleanup"))
				approved = err == nil
			}
			if approved && os.RemoveAll(stage) == nil {
				return
			}
			time.Sleep(250 * time.Millisecond)
		}
	}()
}
