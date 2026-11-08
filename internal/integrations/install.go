package integrations

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

//go:embed releases.json
var releaseManifest []byte

//go:embed licenses/checker.txt
var checkerLicense []byte

//go:embed licenses/knife.txt
var knifeLicense []byte

type archiveSpec struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
}

func archiveFor(engine, goos, arch string) (archiveSpec, string, error) {
	var entries map[string]archiveSpec
	if json.Unmarshal(releaseManifest, &entries) != nil {
		return archiveSpec{}, "", errors.New("TOOL_MANIFEST_INVALID")
	}
	spec, ok := entries[engine+"/"+goos+"/"+arch]
	if !ok {
		return archiveSpec{}, "", errors.New("TOOL_PLATFORM_UNSUPPORTED")
	}
	repo := "kutovoys/xray-checker"
	if engine == "knife" {
		repo = "lilendian0x00/xray-knife"
	}
	return spec, "https://github.com/" + repo + "/releases/download/v" + Version(engine) + "/" + spec.File, nil
}

func Install(ctx context.Context, dataDir, engine string) (Tool, error) {
	spec, downloadURL, err := archiveFor(engine, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return Tool{}, err
	}
	dir := filepath.Join(dataDir, "observations", "tools", engine, Version(engine))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Tool{}, errors.New("PRIVATE_STORAGE_UNAVAILABLE")
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return Tool{}, errors.New("PRIVATE_STORAGE_UNAVAILABLE")
	}
	if os.Chmod(dir, 0o700) != nil {
		return Tool{}, errors.New("PRIVATE_STORAGE_UNAVAILABLE")
	}
	tmp, err := os.MkdirTemp(dir, "install-*")
	if err != nil {
		return Tool{}, errors.New("PRIVATE_STORAGE_UNAVAILABLE")
	}
	defer os.RemoveAll(tmp)
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 3 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		host := req.URL.Hostname()
		if len(via) > 5 || req.URL.Scheme != "https" || host != "github.com" && !strings.HasSuffix(host, ".githubusercontent.com") {
			return errors.New("TOOL_DOWNLOAD_REDIRECT")
		}
		return nil
	}}
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return Tool{}, errors.New("TOOL_DOWNLOAD_FAILED")
	}
	resp, err := client.Do(req)
	if err != nil {
		return Tool{}, errors.New("TOOL_DOWNLOAD_FAILED")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.ContentLength > 256<<20 {
		return Tool{}, errors.New("TOOL_DOWNLOAD_FAILED")
	}
	archive := filepath.Join(tmp, spec.File)
	f, err := os.OpenFile(archive, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return Tool{}, errors.New("PRIVATE_STORAGE_UNAVAILABLE")
	}
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, hash), io.LimitReader(resp.Body, 256<<20+1))
	closeErr := f.Close()
	if copyErr != nil || closeErr != nil || n > 256<<20 {
		return Tool{}, errors.New("TOOL_DOWNLOAD_LIMIT")
	}
	if hex.EncodeToString(hash.Sum(nil)) != spec.SHA256 {
		return Tool{}, errors.New("TOOL_ARCHIVE_HASH_MISMATCH")
	}
	name := "xray-checker"
	if engine == "knife" {
		name = "xray-knife"
	}
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(tmp, name)
	if err := extractBinary(archive, binary, name); err != nil {
		return Tool{}, err
	}
	if err := os.Chmod(binary, 0o700); err != nil {
		return Tool{}, errors.New("TOOL_INSTALL_FAILED")
	}
	data, err := os.ReadFile(binary)
	if err != nil {
		return Tool{}, errors.New("TOOL_INSTALL_FAILED")
	}
	sum := sha256.Sum256(data)
	tool, err := Verify(ctx, Tool{Engine: engine, Path: binary, SHA256: hex.EncodeToString(sum[:])})
	if err != nil {
		return Tool{}, err
	}
	final := filepath.Join(dir, tool.SHA256+"-"+name)
	if _, err := os.Lstat(final); err == nil {
		if err := verifyHash(Tool{Engine: engine, Path: final, SHA256: tool.SHA256}); err != nil {
			return Tool{}, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Tool{}, errors.New("TOOL_INSTALL_FAILED")
	} else if err := os.Rename(binary, final); err != nil {
		return Tool{}, errors.New("TOOL_INSTALL_FAILED")
	}
	tool.Path = final
	license := checkerLicense
	repo := "kutovoys/xray-checker"
	if engine == "knife" {
		license = knifeLicense
		repo = "lilendian0x00/xray-knife"
	}
	if os.WriteFile(filepath.Join(dir, "LICENSE.txt"), license, 0o600) != nil || os.WriteFile(filepath.Join(dir, "UPSTREAM.txt"), []byte("https://github.com/"+repo+"/tree/v"+Version(engine)+"\nArchive SHA-256: "+spec.SHA256+"\n"), 0o600) != nil {
		return Tool{}, errors.New("TOOL_INSTALL_FAILED")
	}
	return tool, nil
}

func extractBinary(archive, dest, name string) error {
	write := func(src io.Reader) error {
		f, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return errors.New("TOOL_ARCHIVE_INVALID")
		}
		n, copyErr := io.Copy(f, io.LimitReader(src, 256<<20+1))
		closeErr := f.Close()
		if copyErr != nil || closeErr != nil || n > 256<<20 {
			return errors.New("TOOL_ARCHIVE_LIMIT")
		}
		return nil
	}
	if strings.HasSuffix(archive, ".zip") {
		r, err := zip.OpenReader(archive)
		if err != nil {
			return errors.New("TOOL_ARCHIVE_INVALID")
		}
		defer r.Close()
		for _, file := range r.File {
			if strings.EqualFold(filepath.Base(file.Name), name) && file.Mode().IsRegular() {
				rc, err := file.Open()
				if err != nil {
					return errors.New("TOOL_ARCHIVE_INVALID")
				}
				err = write(rc)
				_ = rc.Close()
				return err
			}
		}
	} else {
		f, err := os.Open(archive)
		if err != nil {
			return errors.New("TOOL_ARCHIVE_INVALID")
		}
		defer f.Close()
		gz, err := gzip.NewReader(f)
		if err != nil {
			return errors.New("TOOL_ARCHIVE_INVALID")
		}
		defer gz.Close()
		r := tar.NewReader(gz)
		for count := 0; count < 500; count++ {
			header, err := r.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return errors.New("TOOL_ARCHIVE_INVALID")
			}
			if strings.EqualFold(filepath.Base(header.Name), name) && header.Typeflag == tar.TypeReg {
				return write(r)
			}
		}
	}
	return errors.New("TOOL_ARCHIVE_BINARY_MISSING")
}
