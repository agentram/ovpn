package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/skip2/go-qrcode"
)

const userLinkQRCodeSize = 512
const terminalQRCodeQuietZoneCells = 3

func renderTerminalQRCode(link string) (string, error) {
	qr, err := qrcode.New(link, qrcode.Low)
	if err != nil {
		return "", err
	}
	return trimTerminalQRCodeQuietZone(qr.ToSmallString(false), terminalQRCodeQuietZoneCells), nil
}

func trimTerminalQRCodeQuietZone(qrText string, cells int) string {
	if cells <= 0 {
		return qrText
	}
	lines := strings.Split(strings.TrimRight(qrText, "\n"), "\n")
	trimRows := (cells + 1) / 2
	if len(lines) <= trimRows*2 {
		return qrText
	}
	lines = lines[trimRows : len(lines)-trimRows]
	for i, line := range lines {
		runes := []rune(line)
		if len(runes) <= cells*2 {
			return qrText
		}
		lines[i] = string(runes[cells : len(runes)-cells])
	}
	return strings.Join(lines, "\n") + "\n"
}

func writeQRCodePNG(link string, path string) error {
	cleanPath := filepath.Clean(strings.TrimSpace(path))
	if cleanPath == "." || cleanPath == "" {
		return errors.New("qr file path is required")
	}
	parent := filepath.Dir(cleanPath)
	info, err := os.Stat(parent)
	if err != nil {
		return fmt.Errorf("qr file parent directory %s: %w", parent, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("qr file parent path is not a directory: %s", parent)
	}
	png, err := qrcode.Encode(link, qrcode.Medium, userLinkQRCodeSize)
	if err != nil {
		return err
	}
	// #nosec G304 -- operator-provided output path for a generated QR credential.
	f, err := os.OpenFile(cleanPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(png); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Chmod(cleanPath, 0o600)
}
