package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const appBundle = "Browser Extension Manager.app"
const expandedLimit = 256 << 20

var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

type releaseMetadata struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	BuiltAt string `json:"builtAt"`
}

func payloadMetadata(folder, version string) (releaseMetadata, error) {
	var metadata releaseMetadata
	b, err := os.ReadFile(filepath.Join(folder, "release.json"))
	if err != nil || json.Unmarshal(b, &metadata) != nil || metadata.Version != version || !commitPattern.MatchString(metadata.Commit) {
		return metadata, fmt.Errorf("更新包的版本信息与 GitHub Release 不一致")
	}
	if _, err := time.Parse(time.RFC3339, metadata.BuiltAt); err != nil {
		return metadata, fmt.Errorf("更新包缺少有效的构建时间")
	}
	return metadata, nil
}

func unpack(archive, destination string, zipped bool) error {
	if err := os.Mkdir(destination, 0700); err != nil {
		return err
	}
	total, entries := int64(0), 0
	extract := func(name string, size int64, mode os.FileMode, contents io.Reader) error {
		name = strings.TrimSuffix(name, "/")
		local := filepath.FromSlash(name)
		entries++
		if name == "." || path.Clean(name) != name || !filepath.IsLocal(local) ||
			strings.ContainsAny(name, `\:`) || size < 0 || size > expandedLimit-total || entries > 4096 {
			return fmt.Errorf("更新包包含不支持的文件")
		}
		total += size
		if name == "__MACOSX" || strings.HasPrefix(name, "__MACOSX/") {
			// ditto stores Finder metadata separately from the application bundle.
			return nil
		}
		dest := filepath.Join(destination, local)
		if mode.IsDir() {
			return os.MkdirAll(dest, 0755)
		}
		if !mode.IsRegular() {
			return fmt.Errorf("更新包包含不支持的文件类型")
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return err
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644|mode.Perm()&0111)
		if err != nil {
			return err
		}
		n, err := io.Copy(out, io.LimitReader(contents, size+1))
		closeErr := out.Close()
		if err != nil {
			return err
		}
		if n != size {
			return fmt.Errorf("更新包内容不完整")
		}
		return closeErr
	}
	if zipped {
		reader, err := zip.OpenReader(archive)
		if err != nil {
			return err
		}
		defer reader.Close()
		for _, item := range reader.File {
			if item.UncompressedSize64 > expandedLimit {
				return fmt.Errorf("更新包解压后体积过大")
			}
			file, err := item.Open()
			if err != nil {
				return err
			}
			err = extract(item.Name, int64(item.UncompressedSize64), item.Mode(), file)
			file.Close()
			if err != nil {
				return err
			}
		}
		return nil
	}
	input, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer input.Close()
	gz, err := gzip.NewReader(input)
	if err != nil {
		return err
	}
	defer gz.Close()
	reader := tar.NewReader(io.LimitReader(gz, expandedLimit+16<<20))
	for {
		item, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if err := extract(item.Name, item.Size, item.FileInfo().Mode(), reader); err != nil {
			return err
		}
	}
}
