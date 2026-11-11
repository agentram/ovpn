package probequeue

import (
	"context"
	"fmt"
	"ovpn/internal/clientcfg"
	"testing"
	"time"
)

func config(node int) clientcfg.Config {
	return clientcfg.Config{Outbounds: []map[string]any{{"settings": map[string]any{"vnext": []any{map[string]any{"address": fmt.Sprint(node), "users": []any{map[string]any{"id": "fixture-user"}}}}}}}}
}
func TestNativeAndToolChecksShareFourSlotsAndDoNotMakeUsersOnline(t *testing.T) {
	gate := New()
	var release []func()
	for node := range 4 {
		done, err := gate.Acquire(context.Background(), config(node))
		if err != nil {
			t.Fatal(err)
		}
		release = append(release, done)
	}
	if !gate.Masked("fixture-user") {
		t.Fatal("probe was not excluded from presence")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := gate.Acquire(ctx, config(5)); err == nil {
		t.Fatal("fifth check acquired a traffic slot")
	}
	release[0]()
	ctx2, cancel2 := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel2()
	if _, err := gate.Acquire(ctx2, config(1)); err == nil {
		t.Fatal("two checks acquired one server")
	}
	for _, done := range release {
		done()
	}
	if !gate.Masked("fixture-user") {
		t.Fatal("probe closing window was treated as human activity")
	}
}
