# CLI coverage in Desk

The executable catalog is generated from command registrations. A contract test
requires an explicit title and screen area for every runnable user command.
Machine protocol and Cobra help/completion are service exceptions. Common
actions have dedicated controls; every listed action is also available under
Settings → Advanced → search. No terminal output is parsed to execute a command.

| Command | Screen | Action |
| --- | --- | --- |
| `ovpn config render` | Servers | Preview server configuration |
| `ovpn config validate` | Servers | Validate configuration |
| `ovpn deploy` | Servers | Apply server configuration |
| `ovpn doctor` | Servers | Find connection problems |
| `ovpn observe diagnose` | Activity | Run advanced diagnostics |
| `ovpn observe install` | Activity | Install diagnostic tool |
| `ovpn observe once` | Activity | Run custom checks |
| `ovpn observe policy add` | Activity | Add custom checks |
| `ovpn observe policy pause` | Activity | Pause custom checks |
| `ovpn observe policy rm` | Activity | Remove custom checks |
| `ovpn observe run` | Activity | Start continuous checks |
| `ovpn observe status` | Activity | Check history |
| `ovpn observe tool-set` | Activity | Choose diagnostic tool |
| `ovpn restart` | Servers | Restart services |
| `ovpn server add` | Servers | Add server |
| `ovpn server backend attach` | Servers | Add backend |
| `ovpn server backend detach` | Servers | Remove backend |
| `ovpn server backend list` | Servers | Backend pool |
| `ovpn server backup` | Servers | Create backup |
| `ovpn server cleanup` | Servers | Remove OVPN from server |
| `ovpn server ha-apply` | Servers | Apply HA configuration |
| `ovpn server init` | Servers | Install OVPN |
| `ovpn server inspect` | Servers | Check server address |
| `ovpn server list` | Servers | Servers |
| `ovpn server logs` | Servers | View logs |
| `ovpn server monitor down` | Servers | Stop server monitoring |
| `ovpn server monitor status` | Servers | Monitoring status |
| `ovpn server monitor telegram-setup` | Servers | Set up Telegram alerts |
| `ovpn server monitor up` | Servers | Enable server monitoring |
| `ovpn server profile disable` | Servers | Disable connection method |
| `ovpn server profile enable` | Servers | Enable connection method |
| `ovpn server profile list` | Servers | Connection methods |
| `ovpn server profile switch` | Servers | Choose default connection method |
| `ovpn server restore` | Servers | Restore backup |
| `ovpn server set-reality-min-client-version` | Servers | Set client compatibility |
| `ovpn server set-reality-target` | Servers | Change connection destination |
| `ovpn server set-xray-version` | Servers | Change Xray version |
| `ovpn server status` | Servers | Check services |
| `ovpn server test` | Servers | Check SSH access |
| `ovpn stats` | Overview | Traffic totals |
| `ovpn stats sync` | Overview | Refresh traffic |
| `ovpn stats user` | Overview | Daily traffic |
| `ovpn subscription invite` | Users | Create setup code |
| `ovpn subscription revoke` | Users | Revoke profile updates |
| `ovpn subscription serve` | Users | Start profile delivery |
| `ovpn user add` | Users | Add user |
| `ovpn user debug list` | Users | Active recordings |
| `ovpn user debug show` | Users | Connection recording |
| `ovpn user debug start` | Users | Start detailed recording |
| `ovpn user debug stop` | Users | Stop detailed recording |
| `ovpn user diagnose` | Users | Explain connection problem |
| `ovpn user disable` | Users | Disable access |
| `ovpn user enable` | Users | Enable access |
| `ovpn user expiry-clear` | Users | Remove access end date |
| `ovpn user expiry-set` | Users | Set access end date |
| `ovpn user export` | Users | Export access |
| `ovpn user invite` | Users | Create setup code |
| `ovpn user link` | Users | Show connection link |
| `ovpn user list` | Users | Users |
| `ovpn user plan` | Users | Review access change |
| `ovpn user probe` | Users | Check connection |
| `ovpn user qr` | Users | Save QR image |
| `ovpn user quota-reset` | Users | Remove traffic block |
| `ovpn user quota-set` | Users | Change traffic limit |
| `ovpn user reconcile` | Users | Repair user records |
| `ovpn user rm` | Users | Delete user |
| `ovpn user show` | Users | User details |
| `ovpn user subscription export` | Users | Export profile list |
| `ovpn user subscription publish` | Users | Publish profile list |
| `ovpn user top` | Users | Traffic leaders |
| `ovpn version` | Settings | Application version |
