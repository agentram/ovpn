package machine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"ovpn/internal/app/dto"
	"ovpn/internal/machine/protocol"
)

type fakeBackend struct {
	calls int
	fail  bool
}

func (f *fakeBackend) Summary(context.Context) (dto.Summary, error) {
	f.calls++
	if f.fail {
		return dto.Summary{}, errors.New("private-key host.example.net /private/state")
	}
	return dto.Summary{ReadOnly: true}, nil
}
func (f *fakeBackend) Servers(context.Context, string, int) (dto.ServerPage, error) {
	f.calls++
	return dto.ServerPage{Items: []dto.Server{}}, nil
}
func (f *fakeBackend) Check(context.Context, dto.CheckRequest) (dto.Check, error) {
	f.calls++
	return dto.Check{Outcome: "passed"}, nil
}

func serveFixture(t *testing.T, input string, backend Backend) []protocol.Response {
	t.Helper()
	var output bytes.Buffer
	if err := Serve(context.Background(), strings.NewReader(input), &output, backend); err != nil {
		t.Fatal(err)
	}
	var responses []protocol.Response
	for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
		var response protocol.Response
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			t.Fatal(err)
		}
		responses = append(responses, response)
	}
	return responses
}

func TestHandshakeStrictEnvelopeAndReadOnlyMethods(t *testing.T) {
	backend := &fakeBackend{}
	responses := serveFixture(t, `{"id":"a","method":"workspace.summary","params":{}}
{"id":"b","method":"engine.hello","params":{"protocolVersion":2}}
{"id":"c","method":"engine.hello","params":{"protocolVersion":1}}
{"id":"d","method":"servers.list","params":{"limit":2}}
{"id":"e","method":"operations.start","params":{}}
{"id":"f","method":"workspace.summary","params":{"secret":"ignored?"}}
{"id":"g","method":"workspace.summary","params":{},"extra":1}
{"id":"h","method":"checks.run","params":{"serverId":"node-1","username":"monitor"}}
{"id":"i","method":"users.list","params":{"limit":2}}
`, backend)
	want := []string{"HANDSHAKE_REQUIRED", "PROTOCOL_MISMATCH", "", "", "METHOD_NOT_FOUND", "INVALID_PARAMS", "INVALID_REQUEST", "", ""}
	if len(responses) != len(want) {
		t.Fatalf("responses: %d", len(responses))
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
	if backend.calls != 3 {
		t.Fatalf("invalid requests reached backend: %d", backend.calls)
	}
	var hello protocol.Hello
	if err := json.Unmarshal(responses[2].Result, &hello); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(hello.Capabilities, ","), "operations") {
		t.Fatal("advertised writes")
	}
}

func TestRawBackendErrorsNeverCrossProtocol(t *testing.T) {
	responses := serveFixture(t, "{\"id\":\"a\",\"method\":\"engine.hello\",\"params\":{\"protocolVersion\":1}}\n{\"id\":\"b\",\"method\":\"workspace.summary\",\"params\":{}}\n", &fakeBackend{fail: true})
	raw, _ := json.Marshal(responses)
	if strings.Contains(string(raw), "private") || responses[1].Error.Code != "INTERNAL_ERROR" {
		t.Fatal("raw error leaked")
	}
}

func TestOversizedInputStopsWithoutDispatch(t *testing.T) {
	backend := &fakeBackend{}
	var output bytes.Buffer
	if err := Serve(context.Background(), strings.NewReader(strings.Repeat("x", protocol.MaxMessageBytes+1)), &output, backend); err == nil {
		t.Fatal("oversized input accepted")
	}
	if backend.calls != 0 || !strings.Contains(output.String(), "MESSAGE_TOO_LARGE") || output.Len() > 256 {
		t.Fatal("invalid oversized response")
	}
}

func (f *fakeBackend) Users(context.Context, string, int) (dto.UserPage, error) {
	f.calls++
	return dto.UserPage{Items: []dto.User{}}, nil
}

type fakePlanner struct{ fakeBackend }

func (f *fakePlanner) PlanUser(context.Context, dto.UserPlanRequest) (dto.UserPlan, error) {
	f.calls++
	return dto.UserPlan{Scope: "all_enabled_servers", Targets: []dto.UserPlanTarget{}, Issues: []string{}}, nil
}

func TestUserPlansAdvertiseOnlySupportedReadCapabilityAndRejectWriteFields(t *testing.T) {
	backend := &fakePlanner{}
	responses := serveFixture(t, `{"id":"a","method":"engine.hello","params":{"protocolVersion":1}}
{"id":"b","method":"users.plan","params":{"action":"create","username":"alice"}}
{"id":"c","method":"users.plan","params":{"action":"create","username":"alice","apply":true}}
{"id":"d","method":"operations.start","params":{}}
`, backend)
	var hello protocol.Hello
	if err := json.Unmarshal(responses[0].Result, &hello); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(hello.Capabilities, ","), "users.plan.read") || backend.calls != 1 ||
		responses[1].Error != nil || responses[2].Error.Code != "INVALID_PARAMS" || responses[3].Error.Code != "METHOD_NOT_FOUND" {
		t.Fatal("plan capability or strict request boundary incorrect")
	}
	unsupported := serveFixture(t, `{"id":"a","method":"engine.hello","params":{"protocolVersion":1}}
{"id":"b","method":"users.plan","params":{"action":"create","username":"alice"}}
`, &fakeBackend{})
	if unsupported[1].Error.Code != "METHOD_NOT_FOUND" || strings.Contains(string(unsupported[0].Result), "users.plan.read") {
		t.Fatal("advertised an unsupported planner")
	}
}
