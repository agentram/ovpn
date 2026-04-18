Place runtime secrets in this directory on the target server:

- `telegram_bot_token`: Telegram bot token used for polling and notifications.
- `telegram_admin_token`: optional admin token; when present, owner-only `/restart` and `/heal` actions are enabled.

Secret files must not be committed.
