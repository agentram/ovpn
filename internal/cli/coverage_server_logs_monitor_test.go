package cli

import (
	"strings"
	"testing"
)

func TestServerLogsAndTelegramSetupInputErrors(t *testing.T) {
	app := newTestAppWithServer(t, true)

	cases := []struct {
		name string
		args []string
		want string
	}{
		{name: "logs tail", args: []string{"logs", "main", "--tail", "0"}, want: "--tail must be > 0"},
		{name: "logs service", args: []string{"logs", "main", "--service", "unknown"}, want: "unsupported --service"},
		{name: "telegram token", args: []string{"monitor", "telegram-setup", "main", "--token", " "}, want: "telegram token is required"},
		{name: "telegram notify ids", args: []string{"monitor", "telegram-setup", "main", "--token", "token", "--notify-chat-ids", "bad"}, want: "invalid notify chat ids"},
		{name: "telegram owner required", args: []string{"monitor", "telegram-setup", "main", "--token", "token", "--notify-chat-ids", "", "--owner-user-id", ""}, want: "owner user id is required"},
		{name: "telegram owner invalid", args: []string{"monitor", "telegram-setup", "main", "--token", "token", "--owner-user-id", "bad"}, want: "invalid owner user id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearTelegramSetupEnv(t)
			cmd := app.serverCmd()
			cmd.SetArgs(tc.args)
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q for %v, got %v", tc.want, tc.args, err)
			}
		})
	}
}
