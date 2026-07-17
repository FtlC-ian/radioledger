# LoTW Certificates

> Manage tQSL certificates for LoTW signing within the RadioLedger desktop client.

LoTW QSO signing requires a tQSL certificate issued by ARRL. In desktop-local signing mode, the RadioLedger desktop client uses certificates on your machine and does not upload certificate material to the server.

## How Certificates Work

Each LoTW certificate covers a specific callsign and optionally a station location (grid square, DXCC entity). ARRL issues them after verifying your license.

The certificate's private key is stored locally in tQSL. For desktop-local signing, RadioLedger calls the tQSL binary to sign QSOs, and the private key remains local.


## Getting a tQSL Certificate

If you don't have a tQSL certificate:

1. Download tQSL from [lotw.arrl.org](https://lotw.arrl.org)
2. Install tQSL
3. In tQSL: **File → Request New Callsign Certificate**
4. Follow the ARRL verification process (takes 24-72 hours by email)
5. Install the certificate when the email arrives

## Viewing Your Certificates

In the desktop client: **Settings → LoTW → Certificates**

The certificate list shows:
- Callsign
- Station location (if applicable)
- Expiry date
- Status (active/expired/about to expire)

## Configuring LoTW for a Callsign

After installing your certificate in tQSL:

1. Desktop client: **Settings → LoTW**
2. Select your certificate from the list
3. Select a station location (if you have multiple — e.g., home + POTA)
4. Enable **Auto-upload to LoTW**

## Multiple Callsigns / Station Locations

If you operate under multiple callsigns or from multiple locations (common for POTA activators who log each park separately), you can configure multiple station locations:

| Location | Certificate | Callsign | Grid |
|----------|-------------|---------|------|
| Home | W1AW-Home | W1AW | FN42 |
| POTA - K-1234 | W1AW-POTA | W1AW | FN32 |
| Club - W1XYZ | W1XYZ-Home | W1XYZ | FN42 |

When uploading, RadioLedger selects the station location matching the logbook or asks you to confirm.

TODO: Document how station location selection works during upload.

## Certificate Expiry

tQSL certificates expire (typically every 3 years). RadioLedger monitors expiry dates and notifies you at:
- 60 days before expiry
- 30 days before expiry
- 7 days before expiry

**For desktop-local signing, the expiry date may be sent to your own RadioLedger deployment for notification purposes. The certificate content stays on your machine.**

### Renewing an Expired Certificate

1. In tQSL: **File → Renew Callsign Certificate**
2. Follow the ARRL renewal process
3. Install the renewed certificate in tQSL
4. The desktop client detects the new certificate automatically

## Backing Up Certificates

Back up your tQSL certificates! If you lose them, you need to go through the ARRL verification process again.

In tQSL: **File → Export Callsign Certificate** → save the `.p12` file to a secure location (external drive, encrypted backup).

## tQSL Path Configuration

The desktop client auto-detects tQSL on startup. If detection fails:

| Platform | Default path |
|----------|-------------|
| macOS | `/usr/local/bin/tqsl` |
| Windows | `C:\Program Files (x86)\tQSL\tqsl.exe` |
| Linux | `/usr/bin/tqsl` |

Set manually: **Settings → LoTW → tQSL Path**

## Related

- [LoTW Sync](../sync/lotw.md)
- [Desktop Client Overview](index.md)
- [Troubleshooting](troubleshooting.md)
