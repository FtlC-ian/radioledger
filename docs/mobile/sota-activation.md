# SOTA Activation Guide (Mobile)

> Log a Summits on the Air activation from a summit using the RadioLedger mobile app.

## Before You Climb

1. Look up your summit reference at [sotadata.org.uk](https://sotadata.org.uk)
2. Open the app with internet access (sync latest data)
3. Verify SOTA account is connected: **Settings → Connected Services → SOTA**

## Starting a SOTA Activation

At the summit:

1. Tap **+ New Activation** → **SOTA Activation**
2. Search for your summit by reference (e.g., `W0/SP-001`) or name
3. The summit info screen shows: name, elevation, points value, activation history
4. Tap **Start Activation**

## Logging QSOs

The SOTA activation screen is similar to POTA but with SOTA-specific elements:

- **Summit reference**: Shown in the persistent header
- **Altitude**: GPS altitude shown (for summit verification)
- **S2S indicator**: Tap to flag a summit-to-summit contact

### S2S (Summit-to-Summit) QSOs

When you work another SOTA activator from your summit:

1. Enable **S2S** toggle
2. Enter their summit reference
3. Log normally

S2S QSOs earn bonus points and show with an S2S badge in the list.

## SOTA-Specific Considerations

**Altitude**: SOTA requires you to be within the "activation zone" — typically within 25 vertical meters of the true summit. The app shows your GPS altitude for reference.

**Chasers**: When you're the chaser (not activating), use regular logging mode. You can still tag QSOs with the activator's summit reference by entering it in the `SOTA_REF` field.

## Uploading to SOTA

SOTA uses a CSV format, not ADIF. RadioLedger converts automatically.

Tap **Upload to SOTA** after the activation. Select:
- **Activator log** — you were on the summit
- **Chaser log** — you called a SOTA activator from home/portable

TODO: Document SOTA upload flow in detail.

## Related

- [SOTA Log Upload](../sync/sota.md)
- [Awards Tracking (SOTA)](../user-guide/awards-tracking.md)
- [POTA Activation Guide](pota-activation.md)
- [Offline Logging](offline-logging.md)
