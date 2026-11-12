package util

import (
	"net/url"
	"runtime"
	"strings"
)

// FileURL encodes an absolute filesystem path without treating a Windows drive as a URI host.
func FileURL(path string) url.URL { return fileURL(path, runtime.GOOS == "windows") }

func fileURL(path string, windows bool) url.URL {
	if windows {
		path = strings.ReplaceAll(path, `\`, "/")
		if len(path) >= 2 && path[1] == ':' {
			path = "/" + path
		}
	}
	return url.URL{Scheme: "file", Path: path}
}
