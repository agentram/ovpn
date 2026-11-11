package cli

import (
	"sort"
	"strings"

	"ovpn/internal/model"
	"ovpn/internal/operations"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var deskAreas = map[string]string{"user": "users", "subscription": "users", "server": "servers", "deploy": "servers", "restart": "servers", "config": "servers", "stats": "overview", "doctor": "servers", "observe": "activity", "version": "settings"}
var deskRead = map[string]bool{
	"server.inspect": true, "server.test": true, "server.list": true, "server.status": true, "server.profile.list": true, "server.backend.list": true, "server.logs": true, "server.monitor.status": true,
	"user.list": true, "user.show": true, "user.top": true, "user.link": true, "user.diagnose": true, "user.debug.list": true, "user.debug.show": true, "user.plan": true, "user.probe": true,
	"stats": true, "stats.user": true, "config.render": true, "config.validate": true, "doctor": true, "observe.status": true, "version": true, "subscription.serve": true, "observe.run": true,
}
var deskTitles = map[string]string{
	"user.invite": "Create setup code", "user.add": "Add user", "user.rm": "Delete user", "user.enable": "Enable access", "user.disable": "Disable access", "user.quota-set": "Change traffic limit", "user.quota-reset": "Remove traffic block", "user.expiry-set": "Set access end date", "user.expiry-clear": "Remove access end date",
	"user.link": "Show connection link", "user.qr": "Save QR image", "user.export": "Export access", "user.reconcile": "Repair user records", "user.plan": "Review access change", "user.show": "User details", "user.list": "Users", "user.top": "Traffic leaders", "user.probe": "Check connection", "user.diagnose": "Explain connection problem",
	"user.debug.start": "Start detailed recording", "user.debug.stop": "Stop detailed recording", "user.debug.list": "Active recordings", "user.debug.show": "Connection recording",
	"user.subscription.export": "Export profile list", "user.subscription.publish": "Publish profile list", "subscription.invite": "Create setup code", "subscription.revoke": "Revoke profile updates", "subscription.serve": "Start profile delivery",
	"server.inspect": "Check server address", "server.test": "Check SSH access", "server.add": "Add server", "server.init": "Install OVPN", "server.list": "Servers", "server.status": "Check services", "server.backup": "Create backup", "server.restore": "Restore backup", "server.cleanup": "Remove OVPN from server", "server.logs": "View logs",
	"server.set-xray-version": "Change Xray version", "server.set-reality-target": "Change connection destination", "server.set-reality-min-client-version": "Set client compatibility",
	"server.profile.list": "Connection methods", "server.profile.enable": "Enable connection method", "server.profile.disable": "Disable connection method", "server.profile.switch": "Choose default connection method",
	"server.backend.attach": "Add backup route", "server.backend.detach": "Remove backup route", "server.backend.list": "Backup routes",
	"server.monitor.up": "Enable server monitoring", "server.monitor.down": "Stop server monitoring", "server.monitor.status": "Monitoring status", "server.monitor.telegram-setup": "Set up Telegram alerts",
	"deploy": "Apply server configuration", "restart": "Restart services", "doctor": "Find connection problems", "config.render": "Preview server configuration", "config.validate": "Validate configuration", "stats": "Traffic totals", "stats.user": "Daily traffic", "stats.sync": "Refresh traffic",
	"observe.status": "Check history", "observe.run": "Start continuous checks", "observe.install": "Install diagnostic tool", "observe.tool-set": "Choose diagnostic tool", "observe.policy.add": "Add custom checks", "observe.policy.pause": "Pause custom checks", "observe.policy.rm": "Remove custom checks", "observe.once": "Run custom checks", "observe.diagnose": "Run advanced diagnostics", "version": "Application version",
}

func actionID(command *cobra.Command) string {
	return strings.ReplaceAll(strings.TrimPrefix(command.CommandPath(), "ovpn "), " ", ".")
}
func secretField(name string) bool {
	for _, word := range []string{"password", "token", "private-key", "encryption", "uuid", "email"} {
		if strings.Contains(name, word) {
			return true
		}
	}
	return false
}

// commandCatalog is also checked against the complete Cobra command tree.
func commandCatalog(root *cobra.Command) []operations.Action {
	result := []operations.Action{}
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		if cmd.RunE != nil && cmd.Name() != "machine" {
			id := actionID(cmd)
			title := deskTitles[id]
			if title == "" {
				title = cmd.Short
			}
			a := operations.Action{ID: id, Title: title, Area: deskAreas[strings.Split(id, ".")[0]], Description: cmd.Short, Mutation: !deskRead[id], Destructive: id == "user.reconcile" || id == "user.rm" || id == "server.cleanup" || id == "server.restore" || id == "subscription.revoke", Fields: []operations.Field{}, Arguments: []operations.Field{}}
			cmd.Flags().VisitAll(func(flag *pflag.Flag) {
				if flag.Name == "help" {
					return
				}
				required := len(flag.Annotations[cobra.BashCompOneRequiredFlag]) > 0
				secret := secretField(flag.Name)
				value := flag.DefValue
				if secret || flag.Name == "ssh-identity" || flag.Name == "ssh-known-hosts" {
					value = ""
				}
				a.Fields = append(a.Fields, operations.Field{Name: flag.Name, Label: flag.Usage, Type: flag.Value.Type(), Options: fieldOptions(flag.Name), Default: value, Required: required, Secret: secret, Advanced: !required && flag.Name != "ssh-user" && flag.Name != "ssh-identity" && flag.Name != "ssh-port" && flag.Name != "expiry" && flag.Name != "quota-bytes" && flag.Name != "monthly-gb" && flag.Name != "date" && flag.Name != "confirm-traffic" && flag.Name != "engine" && flag.Name != "profile"})
			})
			for _, arg := range strings.Fields(cmd.Use)[1:] {
				if strings.HasPrefix(arg, "<") {
					name := strings.Trim(arg, "<>")
					a.Arguments = append(a.Arguments, operations.Field{Name: name, Label: name, Type: "string", Options: fieldOptions(name), Required: true})
				}
			}
			result = append(result, a)
		}
		for _, child := range cmd.Commands() {
			walk(child)
		}
	}
	walk(root)
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func fieldOptions(name string) []string {
	if name != "profile" {
		return nil
	}
	var out []string
	for _, profile := range model.SupportedTransportProfiles() {
		out = append(out, profile.Name)
	}
	return out
}
