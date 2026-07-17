# Settings

> Configure your account, logbook preferences, display options, and connected services.

## Accessing Settings

Click your callsign / avatar in the top-right corner → **Settings**.

## Account Settings

### Profile

| Setting | Description |
|---------|-------------|
| **Display name** | How your name appears in RadioLedger |
| **Email** | Login email (change via identity provider) |
| **Callsign** | Your primary callsign |
| **Grid square** | Your home station Maidenhead grid |
| **Timezone** | Display timezone for local time conversion |

### Security

- **Change password** — via your identity provider (Zitadel)
- **Active sessions** — view and revoke active login sessions
- **API keys** — generate, view, and revoke API keys for programmatic access

### Notifications

| Notification | Description |
|-------------|-------------|
| LoTW certificate expiry | Alert at 60, 30, and 7 days before cert expires |
| Sync errors | Alert when a sync job fails repeatedly |
| New confirmations | Alert when LoTW/QRZ confirms a QSO |
| Import complete | Alert when an ADIF import finishes |

Notifications can be delivered in-app and/or by email.

## Connected Services

External service credentials (QRZ API keys, eQSL passwords, etc.) are managed under **Connected Services**.

### Credential Verification

When you save credentials for an external service (QRZ, eQSL, ClubLog), RadioLedger **immediately tests them** against the service. You will see:
- **last_verified_at**: The date and time the credentials were last successfully tested.
- **re-verify button**: Manually trigger a verification test at any time.

This ensures your sync configuration is correct before jobs are enqueued.

### Sync Services

Enable or disable which sync services apply to your account. Each service must be configured with valid credentials.

## Logbook Settings

Each logbook has its own settings page. Open a logbook → **Settings** (gear icon).

### General

| Setting | Description |
|---------|-------------|
| **Name** | Logbook display name |
| **Description** | Optional description |
| **Station callsign** | Primary callsign used in this logbook |
| **Default operator** | Default operator for new QSOs |

### Logging Defaults

Pre-fill values that apply to most QSOs in this logbook:

| Setting | Description |
|---------|-------------|
| **Default mode** | Pre-fill mode (e.g., SSB, FT8) |
| **Default power** | Pre-fill TX power in watts |
| **Default antenna** | Pre-fill antenna description |
| **Default band** | Pre-fill band |

### Deduplication

| Setting | Default | Description |
|---------|---------|-------------|
| **Dedup window** | 30 seconds | Time window for duplicate detection |
| **Dedup strictness** | Standard | Standard (callsign + band + mode + datetime) or Relaxed (callsign + datetime only) |

## Display Settings

| Setting | Default | Description |
|---------|---------|-------------|
| **Theme** | System | Light / Dark / System |
| **Date format** | YYYY-MM-DD | Date display format |
| **Time format** | UTC | UTC or Local (with timezone label) |
| **Logbook columns** | Default set | Which columns to show in the QSO list |
| **Rows per page** | 50 | Pagination size |

## Data and Privacy

| Option | Description |
|--------|-------------|
| **Export my data** | Download all your data as ADIF + JSON |
| **Delete my account** | Permanently delete account and all data |

## Self-Hosting: Admin Settings

For self-hosted instances, administrators have access to additional settings:

- User management
- Email configuration (SMTP)
- Storage configuration
- Job queue dashboard (River)

See [Self-Hosting Configuration](../self-hosting/configuration.md) for environment variables and more.

## Related

- [Getting Started: Connect Services](../getting-started/connect-services.md)
- [Logbooks](logbooks.md)
- [API Keys](../api/authentication.md)
- [Self-Hosting Configuration](../self-hosting/configuration.md)
