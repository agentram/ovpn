package machine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ovpn/internal/app"
	"ovpn/internal/machine/protocol"
)

func TestObservationRPCsRequireConsentAndStrictFieldsWithoutCreatingState(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")
	backend := app.NewDesktopService(context.Background(), dir)
	defer backend.Close()
	responses := serveFixture(t, `{"id":"a","method":"engine.hello","params":{"protocolVersion":1}}
{"id":"b","method":"observer.status","params":{}}
{"id":"c","method":"observer.status","params":{"start":true}}
{"id":"d","method":"observer.configure","params":{"engine":"native","targets":[{"serverId":"node-1","username":"monitor","profile":"vless-reality-tcp-vision"}],"intervalSeconds":300,"maxChecksPerDay":300,"enabled":true,"trafficConfirmed":false}}
{"id":"e","method":"observer.run","params":{"policyId":"missing","trafficConfirmed":false}}
{"id":"f","method":"diagnostics.start","params":{"engine":"knife-xray","targets":[{"serverId":"node-1","username":"monitor","profile":"vless-reality-tcp-vision"}],"trafficConfirmed":false}}
{"id":"g","method":"integrations.install","params":{"engine":"checker","url":"https://example.com/custom"}}
{"id":"h","method":"observer.stop","params":{}}
{"id":"i","method":"operations.start","params":{}}
`, backend)
	want := []string{"", "", "INVALID_PARAMS", "TRAFFIC_CONFIRMATION_REQUIRED", "TRAFFIC_CONFIRMATION_REQUIRED", "TRAFFIC_CONFIRMATION_REQUIRED", "INVALID_PARAMS", "", "METHOD_NOT_FOUND"}
	if len(responses) != len(want) {
		t.Fatal("missing responses")
	}
	for i, code := range want {
		got := ""
		if responses[i].Error != nil {
			got = responses[i].Error.Code
		}
		if got != code {
			t.Fatalf("response %d: %s", i, got)
		}
	}
	var hello protocol.Hello
	if err := json.Unmarshal(responses[0].Result, &hello); err != nil {
		t.Fatal(err)
	}
	caps := strings.Join(hello.Capabilities, ",")
	for _, capability := range []string{"observer.read", "observer.configure", "observer.run", "integrations.install", "diagnostics.run"} {
		if !strings.Contains(caps, capability) {
			t.Fatal("missing observer capability", capability)
		}
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("rejected traffic created state")
	}
	unsupported := serveFixture(t, `{"id":"a","method":"engine.hello","params":{"protocolVersion":1}}
{"id":"b","method":"observer.status","params":{}}
`, &fakeBackend{})
	if unsupported[1].Error == nil || unsupported[1].Error.Code != "METHOD_NOT_FOUND" || strings.Contains(string(unsupported[0].Result), "observer.read") {
		t.Fatal("unsupported observation advertised")
	}
}
