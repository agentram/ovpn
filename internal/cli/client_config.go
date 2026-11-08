package cli

import (
	"os"
	"path/filepath"

	"ovpn/internal/clientcfg"
	"ovpn/internal/clientprofile"
	"ovpn/internal/model"
)

func validateClientCheckUser(srv model.Server, user model.User) error {
	return clientprofile.ValidateCheckUser(srv, user)
}
func clientConfigForUser(srv model.Server, user model.User, profile string, opts userLinkOptions) (clientcfg.Config, error) {
	return clientprofile.ConfigForUser(srv, user, profile, opts.shared())
}

func writePrivateClientConfig(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".ovpn-client-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
