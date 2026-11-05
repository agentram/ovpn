package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"ovpn/internal/clientcfg"
	"ovpn/internal/model"
)

func validateClientCheckUser(srv model.Server, user model.User) error {
	if !srv.Enabled || !user.Enabled || model.IsExpiredAt(user.ExpiryDate, time.Now().UTC()) || user.QuotaBlocked {
		return fmt.Errorf("client checks require an enabled server and an active user")
	}
	return nil
}

func clientConfigForUser(srv model.Server, user model.User, profile string, opts userLinkOptions) (clientcfg.Config, error) {
	if err := validateClientCheckUser(srv, user); err != nil {
		return clientcfg.Config{}, err
	}
	link, err := buildUserProfileLink(srv, user, profile, opts)
	if err != nil {
		return clientcfg.Config{}, err
	}
	return clientcfg.FromLink(link)
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
