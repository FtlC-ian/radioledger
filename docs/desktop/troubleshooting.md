# Desktop Client Troubleshooting

> Solutions for common desktop client issues.

## Can't Log In

**Browser doesn't open?**
- Try right-clicking the system tray icon → **Log In**
- If no browser opens, copy the login URL from the desktop client settings and open it manually

**"Redirect URI mismatch" error in browser?**
- This usually means the loopback listener on a random port failed to bind
- Try restarting the desktop client and logging in again
- Check that no other application is blocking loopback connections

**Login succeeds but desktop client doesn't reconnect?**
- Firewall may be blocking localhost traffic on the callback port
- Allow RadioLedger in your firewall settings

## UDP / Auto-Logging Not Working

**QSOs from WSJT-X not appearing?**
1. Verify the desktop client is running (system tray icon)
2. Check WSJT-X reporting settings: `127.0.0.1:2237`
3. Check desktop client: **Settings → UDP → WSJT-X → Enabled**
4. View logs: **Help → Show Logs** — look for UDP listener errors
5. Try restarting both WSJT-X and the desktop client

**"Address already in use" error?**
- Another application is using the same UDP port
- Change the port in desktop client settings to 2238 (or any free port)
- Update WSJT-X to send to the new port

**QSOs appearing twice?**
- Increase the deduplication window in Logbook Settings to 60 seconds
- Check if both WSJT-X's "UDP" and "ADIF" outputs are enabled simultaneously — disable one

## LoTW Not Uploading

**"tQSL not found"?**
- Set the tQSL path manually: **Settings → LoTW → tQSL Path**
- Common paths: `/usr/local/bin/tqsl` (macOS), `C:\Program Files (x86)\tQSL\tqsl.exe` (Windows)

**"No certificates found"?**
- Open tQSL directly and verify certificates are installed
- After installing in tQSL, restart the desktop client to refresh the certificate list

**"Certificate expired"?**
- Renew your certificate at lotw.arrl.org
- See [LoTW Certificates](lotw-certificates.md)

**LoTW server unreachable?**
- LoTW is sometimes unavailable during contest weekends
- RadioLedger will retry automatically; check sync status page

## Rig Control Not Working

**Frequency not updating?**
- Verify Flrig or rigctld is running
- Check the port matches (Flrig: 12345, Hamlib: 4532)
- Some radios don't report mode — check your radio's Flrig/Hamlib support

**Wrong frequency?**
- Check if VFO A vs VFO B is selected
- Verify your rig is connected and responding in Flrig

## Server Connection Issues

**Desktop client shows "Offline"?**
- Check your internet connection
- For self-hosted: verify the API server is running (`docker compose ps`)
- Check the server URL in settings

**QSOs logged offline not syncing?**
- Desktop client has an offline queue that syncs when connectivity is restored
- Check the pending queue in **Settings → Sync Status**

## Log Files

Desktop client logs are at:

| Platform | Log location |
|----------|-------------|
| macOS | `~/Library/Logs/RadioLedger/radioledger.log` |
| Windows | `%APPDATA%\RadioLedger\logs\radioledger.log` |
| Linux | `~/.radioledger/logs/radioledger.log` |

Or access via: **Help → Show Logs**

## Resetting the Desktop Client

If you need a clean reset (clears local cache, config, and removes tokens):

1. Sign out: system tray → **Sign Out**
2. Delete the config directory (see log file location above — config is in the same parent folder)
3. Delete RadioLedger entries from your OS keychain (macOS Keychain Access, Windows Credential Manager, or `secret-tool` on Linux)
4. Relaunch and run the setup wizard again

## Getting Help

- GitHub Issues: [github.com/FtlC-ian/radioledger/issues](https://github.com/FtlC-ian/radioledger/issues)
- Include your operating system, desktop client version (**Help → About**), and relevant log lines

## Related

- [Desktop Client Overview](index.md)
- [WSJT-X Setup](wsjtx-setup.md)
- [LoTW Certificates](lotw-certificates.md)
- [FAQ](../faq.md)
