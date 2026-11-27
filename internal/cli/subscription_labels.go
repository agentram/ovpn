package cli

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"ovpn/internal/model"
)

const subscriptionLabelsFile = "profile-labels.json"

var errSubscriptionLabels = errors.New("SUBSCRIPTION_LABELS_INVALID")

type subscriptionLabels map[string]struct {
	Name     string            `json:"name"`
	Profiles map[string]string `json:"profiles"`
}

func (a *App) readSubscriptionLabels() (subscriptionLabels, error) {
	const maxSize = 64 << 10
	path := filepath.Join(a.dataDir, "delivery", subscriptionLabelsFile)
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxSize {
		return nil, errSubscriptionLabels
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errSubscriptionLabels
	}
	defer file.Close()

	decoder := json.NewDecoder(io.LimitReader(file, maxSize+1))
	decoder.DisallowUnknownFields()
	var labels subscriptionLabels
	if decoder.Decode(&labels) != nil || labels == nil || decoder.Decode(new(any)) != io.EOF {
		return nil, errSubscriptionLabels
	}
	for node, label := range labels {
		if strings.TrimSpace(node) == "" || (label.Name != "" && !validSubscriptionLabel(label.Name)) {
			return nil, errSubscriptionLabels
		}
		for id, name := range label.Profiles {
			profile, known := model.LookupTransportProfile(id)
			if !known || profile.Name != id || !validSubscriptionLabel(name) {
				return nil, errSubscriptionLabels
			}
		}
	}
	return labels, nil
}

func validSubscriptionLabel(value string) bool {
	return strings.TrimSpace(value) != "" && utf8.RuneCountInString(value) <= 64 &&
		strings.IndexFunc(value, unicode.IsControl) == -1
}

func (labels subscriptionLabels) fragment(node, profile string) string {
	label := labels[node]
	if name := label.Profiles[profile]; name != "" {
		profile = name
	}
	if label.Name != "" {
		node = label.Name
	}
	return node + " · " + profile
}
