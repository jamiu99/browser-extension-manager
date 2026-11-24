package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type releaseTransport map[string]string

func (t releaseTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body, ok := t[req.URL.String()]
	if !ok {
		return nil, fmt.Errorf("unexpected request: %s", req.URL)
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
}

func fixtureService(tag, name, body string) Service {
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(body)))
	sums := digest + "  " + name + "\n"
	release, _ := json.Marshal(map[string]any{
		"tag_name": tag,
		"assets": []asset{
			{Name: name, URL: downloadURL(tag, name), State: "uploaded", Size: int64(len(body)), Digest: "sha256:" + digest},
			{Name: checksumName, URL: downloadURL(tag, checksumName), State: "uploaded", Size: int64(len(sums))},
		},
	})
	return Service{&http.Client{Transport: releaseTransport{
		latestURL:                      string(release),
		downloadURL(tag, name):         body,
		downloadURL(tag, checksumName): sums,
	}}}
}

func TestLatestStableDoesNotDowngradePreview(t *testing.T) {
	for _, test := range []struct{ current, latest, state string }{
		{"v0.2.1-dev", "v0.1.1", "ahead"},
		{"v0.2.1-dev", "v0.2.1", "available"},
		{"v1.2.10", "v1.2.3", "ahead"},
		{"v1.2.10", "v1.2.10", "current"},
		{"v1.2.9", "v1.2.10", "available"},
		{"dev", "v1.2.10", "development"},
		{"v1.2.9", "v1.2.10-beta", "invalid"},
	} {
		if actual := compare(test.current, test.latest); actual != test.state {
			t.Errorf("compare(%q, %q) = %q, want %q", test.current, test.latest, actual, test.state)
		}
	}
}

func TestReleaseSelectsAndChecksPlatformAsset(t *testing.T) {
	ctx := context.Background()
	tag := "v1.2.0"
	name := "extension-manager-v1.2.0-windows-x64.zip"
	s := fixtureService(tag, name, "download")
	result, err := s.check(ctx, "v1.1.0", "windows", "amd64")
	if err != nil || result.State != "available" || result.asset.Name != name {
		t.Fatalf("unexpected release: %+v, %v", result, err)
	}
	path := filepath.Join(t.TempDir(), "package")
	if err := s.download(ctx, result, path); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "download" {
		t.Fatalf("unexpected downloaded package: %q %v", b, err)
	}
	s.client.Transport.(releaseTransport)[downloadURL(tag, name)] = "Download"
	if err := s.download(ctx, result, path+"-corrupt"); err == nil {
		t.Fatal("download with a bad checksum was accepted")
	}
	if result, err := s.check(ctx, "v1.1.0", "linux", "amd64"); err != nil || result.State != "manual" {
		t.Fatalf("unsupported release status: %+v, %v", result, err)
	}
}
