# RadioLedger Confirmation System
## How to make LoTW irrelevant

---

## How LoTW Works (and why it sucks)

**The concept is simple:**
1. Operator A uploads a QSO record (I worked W5ABC on 20m SSB at 14:32 UTC)
2. Operator B uploads their side (I worked KI5BRG on 20m SSB at 14:32 UTC)
3. LoTW matches them → "Confirmed"
4. Confirmed QSOs count toward ARRL awards (DXCC, WAS, VUCC, etc.)

**The identity verification:**
- You request a certificate from ARRL
- They mail a postcard to your FCC address with a verification code
- You enter the code, get a digital certificate (TQ6 format)
- You sign your log uploads with this certificate
- This proves "the person at the FCC address for this callsign uploaded this"

**Why it sucks:**
- Registration takes 1-3 weeks (mail!)
- Certificate management is desktop-only (tqsl application)
- Certificate expires, renewal is painful
- Interface hasn't changed since ~2003
- ARRL controls it unilaterally
- No API worth mentioning
- If ARRL's servers go down, confirmation stops

---

## RadioLedger's Confirmation System

### Core Concept: Same Matching, Better Everything

The matching logic is identical — two sides of a QSO must agree. The difference is everything around it.

### Identity Verification

Instead of mailing postcards, verify identity through:

**Tier 1 — Email Match (instant)**
- User registers with email
- We check if their callsign's FCC record has a matching email (via FRN lookup)
- If yes, verified. If no, fall back to Tier 2.

**Tier 2 — Address Verification (fast)**
- Send a verification code to the FCC-registered address
- But do it via email to the FCC-registered email on the ULS (many hams have email in FCC records)
- Fallback: physical postcard (same as LoTW but we make it faster)

**Tier 3 — Cross-Verification (clever)**
- If operator is already verified on LoTW, QRZ, or ClubLog...
- And they link those accounts to RadioLedger...
- We can trust their identity (transitive trust)
- "You're already verified on LoTW? We'll accept that."

**Tier 4 — Community Vouching (future)**
- Verified operators can vouch for others they know personally
- Web of trust model (like PGP key signing)
- Requires N vouches from verified operators

### Matching Algorithm

```
For each uploaded QSO:
  1. Normalize fields (callsign uppercase, band from freq, UTC time)
  2. Look for matching QSO from the other side:
     - Same callsign pair (A worked B, B worked A)
     - Same band
     - Same mode (with fuzzy matching: USB/LSB → SSB, FT8 → FT8)
     - Time within ±30 minutes (configurable, LoTW uses ±30min)
  3. If match found and both operators are verified:
     → Status: CONFIRMED
  4. If match found but one/both unverified:
     → Status: MATCHED (upgrade to CONFIRMED when verified)
  5. If no match:
     → Status: UNCONFIRMED
```

### Real-Time Confirmation

This is where we crush LoTW:

- LoTW: upload batch → wait for other side to upload batch → confirmation appears days/weeks later
- RadioLedger: both sides on the platform? **Confirmation is instant.**

When Operator A logs a QSO:
1. Check if Operator B is on RadioLedger
2. If yes, check if B already logged their side
3. If yes → instant confirmation, notify both operators
4. If no → queue, notify B they have a pending confirmation ("KI5BRG worked you on 20m, confirm?")

Push notifications, email digest, in-app badge. "You have 3 QSOs waiting for confirmation."

### QSO Confirmation States

```
UNCONFIRMED  → No matching record from other side
PENDING      → We found a likely match, awaiting verification
MATCHED      → Both sides agree, but one/both unverified
CONFIRMED    → Both sides agree AND both verified
REJECTED     → One side disputes the QSO
```

---

## Database Schema

```sql
-- +goose Up

-- Confirmation records linking two QSO entries
CREATE TABLE qso_confirmations (
    id                  BIGSERIAL PRIMARY KEY,
    
    -- The two sides of the QSO
    qso_id              BIGINT NOT NULL REFERENCES qsos(id),
    matched_qso_id      BIGINT REFERENCES qsos(id),  -- NULL if unmatched
    
    -- Denormalized for fast lookups
    our_callsign        TEXT NOT NULL,
    their_callsign      TEXT NOT NULL,
    band                TEXT NOT NULL,
    mode                TEXT NOT NULL,
    qso_date            DATE NOT NULL,
    qso_time            TIME NOT NULL,
    
    -- Confirmation status
    status              TEXT NOT NULL DEFAULT 'unconfirmed',
    -- 'unconfirmed', 'pending', 'matched', 'confirmed', 'rejected'
    
    -- Verification levels
    our_verification    TEXT NOT NULL DEFAULT 'none',
    their_verification  TEXT NOT NULL DEFAULT 'none',
    -- 'none', 'email', 'address', 'cross_verified', 'vouched'
    
    -- External confirmations (synced from other services)
    lotw_confirmed      BOOLEAN DEFAULT FALSE,
    lotw_confirmed_at   TIMESTAMPTZ,
    eqsl_confirmed      BOOLEAN DEFAULT FALSE,
    eqsl_confirmed_at   TIMESTAMPTZ,
    qrz_confirmed       BOOLEAN DEFAULT FALSE,
    qrz_confirmed_at    TIMESTAMPTZ,
    
    -- RadioLedger native confirmation
    rl_confirmed        BOOLEAN DEFAULT FALSE,
    rl_confirmed_at     TIMESTAMPTZ,
    
    confirmed_at        TIMESTAMPTZ,  -- earliest confirmation from any source
    
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_confirmations_qso ON qso_confirmations (qso_id);
CREATE INDEX idx_confirmations_matched ON qso_confirmations (matched_qso_id);
CREATE INDEX idx_confirmations_callsigns ON qso_confirmations (our_callsign, their_callsign);
CREATE INDEX idx_confirmations_status ON qso_confirmations (status);
CREATE INDEX idx_confirmations_date ON qso_confirmations (qso_date);

-- Verification records for operators
CREATE TABLE operator_verifications (
    id              BIGSERIAL PRIMARY KEY,
    user_id         BIGINT NOT NULL REFERENCES users(id),
    callsign        TEXT NOT NULL,
    
    method          TEXT NOT NULL,  -- 'email', 'address', 'lotw_cross', 'qrz_cross', 'vouch'
    status          TEXT NOT NULL DEFAULT 'pending',  -- 'pending', 'verified', 'expired', 'revoked'
    
    -- Method-specific data
    verification_code   TEXT,
    verified_at         TIMESTAMPTZ,
    expires_at          TIMESTAMPTZ,
    
    -- For vouch method
    vouched_by          BIGINT REFERENCES users(id),
    
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_verification_active ON operator_verifications (user_id, callsign, method) 
    WHERE status = 'verified';
```

---

## API Endpoints

```
GET  /v1/confirmations              → List your confirmations (with filters)
GET  /v1/confirmations/pending      → QSOs waiting for your confirmation
POST /v1/confirmations/{id}/confirm → Confirm a matched QSO
POST /v1/confirmations/{id}/reject  → Dispute a matched QSO
GET  /v1/confirmations/stats        → Confirmation rate, sources breakdown

POST /v1/verify/email               → Start email verification
POST /v1/verify/email/confirm       → Complete email verification with code
POST /v1/verify/cross/{service}     → Cross-verify via LoTW/QRZ account linking
GET  /v1/verify/status              → Your verification status
```

---

## The "Gravity Well" Strategy

The confirmation system creates a network effect:

1. **Operator A joins RadioLedger** because the logging UX is great
2. **A's QSOs sit unconfirmed** because the other side isn't on RadioLedger yet
3. **RadioLedger sends B an email:** "KI5BRG logged a QSO with you. Confirm it on RadioLedger."
4. **B signs up** to confirm (free, takes 30 seconds)
5. **B discovers** the platform is actually good and starts logging there too
6. **B's contacts get the same email...**

This is exactly how LinkedIn grew. "Someone viewed your profile" → sign up → view others → they sign up.

The key: the email must be useful and not spammy. One email per batch, not per QSO. "You have 12 QSOs waiting for confirmation on RadioLedger" with a one-click confirm link.

### Making It Not Annoying

- First email: genuinely useful ("someone wants to confirm a QSO with you")
- Max 1 email per week per non-member
- One-click unsubscribe
- Never sell the email, never spam
- If they confirm without creating an account → still works (tokenized link)
- If they create an account → they're in the ecosystem

---

## Award Integration

Once we have confirmations, award tracking becomes automatic:

```
DXCC:  Count confirmed QSOs with unique DXCC entities
WAS:   Count confirmed QSOs with unique US states
VUCC:  Count confirmed QSOs with unique grid squares (VHF/UHF)
WAZ:   Count confirmed QSOs with unique CQ zones
WPX:   Count confirmed QSOs with unique prefixes
```

LoTW confirmations count. RadioLedger native confirmations count. Both are tracked.

"You need 3 more states for WAS. Here are the states you're missing: Alaska, Hawaii, Wyoming."

That's the kind of intelligence that makes people stay.

---

## Timeline

### Phase 1: Matching Engine (Week 1)
- QSO matching algorithm (River worker)
- Confirmation status tracking
- Basic confirmation API

### Phase 2: Verification (Week 2)
- Email verification flow
- Cross-verification (LoTW/QRZ account linking)
- Verification status display

### Phase 3: Notifications (Week 3)  
- "Pending confirmation" emails to non-members
- In-app confirmation dashboard
- Push notifications for instant matches

### Phase 4: Award Tracking Integration (Week 4)
- Link confirmations to award progress
- "X more for DXCC" intelligence
- Award badges on profile pages

---

## Contextual Profile Pages & Shadow Profiles

### The Hook: "47 operators are waiting"

Every callsign in the FCC database gets a page on RadioLedger, claimed or not. But what makes it special is that RadioLedger users' logs populate data about unclaimed callsigns.

### Navigation Context

When a logged-in user clicks a callsign from their own logbook, the profile page shows:
- **Their QSOs with that station** (this is the viewer's own data — no privacy issue)
- **Aggregate stats**: "12 other RadioLedger operators have also worked W5ABC"
- **No details** about other people's QSOs — just the count
- **CTA**: "Is this your callsign? Claim it and confirm these QSOs."

When browsing from search or direct URL (not from a logbook):
- FCC regulatory data (name, location, class, grid)
- Aggregate only: "Worked by 47 RadioLedger operators"
- Bands/modes summary (derived from others' logs): "Most active on 20m FT8"
- No individual QSO details exposed

### Claim Flow with Cascade Confirmations

1. W5ABC finds their page (Google, link from a friend, email notification)
2. Clicks "Claim this callsign" → email verification
3. Uploads their log (ADIF)
4. **Cascade**: matching engine runs against ALL existing QSOs mentioning W5ABC
5. Every match triggers confirmation for both sides
6. Every RadioLedger user who worked W5ABC gets notified: "W5ABC just joined — 3 of your QSOs are now confirmed!"
7. Dopamine. Network effect. Viral loop.

The CTA: **"47 operators are waiting to confirm QSOs with you."**

### Privacy Levels

Three visibility tiers — controls what's *displayed*, never what's *functional*:

| Level | Profile visible | Logbook visible | Confirmations work |
|-------|----------------|-----------------|-------------------|
| **Public** | Yes | Full logbook | Yes |
| **Confirmed only** | Yes | Only confirmed QSOs | Yes |
| **Private** | Minimal (callsign + grid) | Hidden | **Yes** |

**Default: Private.** Users opt into visibility.

Key insight: even a completely private user adds value to the network. Their uploads still confirm other people's QSOs. The matching engine runs on data regardless of display preferences. This means:
- Privacy-conscious old timers join for confirmations without exposing anything
- New hams who want to show off go public
- Everyone benefits from every new member regardless of their privacy setting

This is exactly how LoTW works — nobody can browse your LoTW log, but your uploads still confirm contacts. RadioLedger just makes it explicit and gives you the controls.

---

## Why This Replaces LoTW

| | LoTW | RadioLedger |
|---|---|---|
| Registration | Mail postcard, wait weeks | Email verification, instant |
| Upload | Desktop tqsl app only | Web, desktop, mobile, API |
| Confirmation speed | Days to weeks | Instant if both on platform |
| Interface | 2003 web design | Modern, clean |
| Award tracking | Basic counters | Rich dashboards, maps, progress |
| Multi-service | LoTW only | Aggregates LoTW + QRZ + eQSL + native |
| Cost | Free (ARRL funded) | Free |
| Self-hosted | No | Yes |

The key: never ask people to stop using LoTW. Sync with it automatically. Over time, the native RadioLedger confirmations become the primary ones, and LoTW becomes "that thing I used to use."
