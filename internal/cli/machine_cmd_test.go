package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMachineCommandDoesNotInitializeWorkspace(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")
	cmd := NewRootCmd()
	cmd.SetArgs([]string{"--data-dir", dir, "machine", "--stdio", "--protocol", "1"})
	cmd.SetIn(strings.NewReader("{\"id\":\"hello\",\"method\":\"engine.hello\",\"params\":{\"protocolVersion\":1}}\n{\"id\":\"summary\",\"method\":\"workspace.summary\",\"params\":{}}\n"))
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"state":"empty"`) || !strings.Contains(out.String(), `"protocolVersion":1`) {
		t.Fatal("missing protocol response")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("machine command created local state")
	}
}
