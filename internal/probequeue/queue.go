// Package probequeue shares the traffic budget and presence mask across native,
// Checker and Knife checks in the worker process.
package probequeue

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"ovpn/internal/clientcfg"
)

type Gate struct {
	mu         sync.Mutex
	active     int
	nodes      map[string]bool
	identities map[string]int
	quiet      map[string]time.Time
	changed    chan struct{}
}

func New() *Gate {
	return &Gate{nodes: map[string]bool{}, identities: map[string]int{}, quiet: map[string]time.Time{}, changed: make(chan struct{})}
}

var Shared = New()

func peer(config clientcfg.Config) (string, string) {
	var projection struct {
		Outbounds []struct {
			Settings struct {
				Vnext []struct {
					Address string `json:"address"`
					Users   []struct {
						ID string `json:"id"`
					} `json:"users"`
				} `json:"vnext"`
			} `json:"settings"`
		} `json:"outbounds"`
	}
	raw, _ := config.JSON()
	_ = json.Unmarshal(raw, &projection)
	if len(projection.Outbounds) > 0 && len(projection.Outbounds[0].Settings.Vnext) > 0 {
		p := projection.Outbounds[0].Settings.Vnext[0]
		if len(p.Users) > 0 {
			return p.Address, p.Users[0].ID
		}
		return p.Address, ""
	}
	return "unknown", ""
}
func (g *Gate) Acquire(ctx context.Context, config clientcfg.Config) (func(), error) {
	node, identity := peer(config)
	for {
		g.mu.Lock()
		if g.active < 4 && !g.nodes[node] {
			g.active++
			g.nodes[node] = true
			g.identities[identity]++
			g.mu.Unlock()
			return sync.OnceFunc(func() {
				g.mu.Lock()
				defer g.mu.Unlock()
				g.active--
				delete(g.nodes, node)
				g.identities[identity]--
				g.quiet[identity] = time.Now().Add(30 * time.Second)
				for id, until := range g.quiet {
					if time.Now().After(until) {
						delete(g.quiet, id)
					}
				}
				close(g.changed)
				g.changed = make(chan struct{})
			}), nil
		}
		changed := g.changed
		g.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-changed:
		}
	}
}
func (g *Gate) Masked(identity string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.identities[identity] > 0 || time.Now().Before(g.quiet[identity])
}
