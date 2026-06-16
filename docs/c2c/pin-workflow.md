# eGK PIN workflow

How the cardholder obtains the PIN, what arrives, the counter mechanics
that drive the safety guard in this repo, and the practical "next steps"
checklist for the user.

This page is informational — no code references material from here directly,
but several decisions (the `-UNSAFE-allow-pin-write` flag, the deferral
of VERIFY until C2C completes) are motivated by what's written here.

## The PIN does not come with the card

An eGK is shipped to the cardholder by their Krankenkasse (statutory
health insurer — e.g. Techniker Krankenkasse, AOK, Barmer, …) usable
out of the box for **billing reads only**. EF.PD (personal data) and
EF.VD (insurance data) are publicly readable without authentication —
that's how the practice's billing software gets your name and IK on
visit one.

PIN-protected segments — NFD (Notfalldaten / emergency data), DPE
(persönliche Erklärungen / advance directives), eMP (Medikationsplan),
eRezept access, Notfalldatensatz-Management — are gated behind a separate
**eGK PIN** that the cardholder must **actively request**.

## How to get the PIN

The cardholder requests a PIN from their Krankenkasse via one of:

- **Online via the Krankenkasse's app** (after one-time identity verification —
  TK-App, AOK App, Barmer App, etc.)
- **Online via the Krankenkasse's web portal**
- **In-person at a Geschäftsstelle** (branch office)
- **PostIdent / VideoIdent** with the Krankenkasse's identity provider

After identity verification, the Krankenkasse sends a **PIN-Brief**
(PIN letter) by **Einschreiben** (registered mail with delivery
confirmation) to the cardholder's address on file. Typical lead time:
**1–2 weeks**.

### TK specifically (Techniker Krankenkasse)

- App-based path: log into the TK-App → "Versichertenausweis" / "TK-Safe"
  section → request PIN (referred to as "Gesundheits-PIN" or "eGK-PIN").
- Web path: tk.de account, similar menu.
- Identity verification: VideoIdent (Postbank / IDnow) the first time,
  if not done previously.

(Identical patterns at most Krankenkassen — they use the same gematik
spec underneath, just different brand wrappers.)

## What arrives in the PIN-Brief

Two values:

| Item | Length | Counter | Purpose |
|---|---|---|---|
| **PIN** | 6 decimal digits | 3 strikes before block | Unlocks PIN-protected reads via VERIFY APDU |
| **PUK** | 10 decimal digits | 10 strikes before permanent lock | Unblocks the PIN after it's blocked, via RESET RETRY COUNTER APDU |

Initial PIN behaviour depends on the card profile:

- **Transport PIN**: card refuses VERIFY until the cardholder runs
  CHANGE REFERENCE DATA to set a personal PIN (gemSpec mandates this for
  some profiles).
- **Active PIN**: usable immediately as delivered.

The Krankenkasse can tell the cardholder which they're getting.

## Counter math (and why the safety guard exists)

ISO 7816-4 + gemSpec_COS counter semantics, applied to the eGK PIN:

```
PIN counter: starts at 3.
  Wrong VERIFY → counter -= 1
  Correct VERIFY → counter reset to 3
  Counter hits 0 → PIN blocked. VERIFY returns 6983.

PUK counter: starts at 10.
  Wrong RESET RETRY COUNTER → counter -= 1
  Correct RESET RETRY COUNTER (with the right PUK) → PIN counter back to 3
  Counter hits 0 → PUK permanently exhausted. Card is sealed for
                   PIN-protected reads. Billing reads still work.
```

Three properties are worth internalising:

1. **Counters do not decay.** Three wrong PIN attempts spread over years
   are equivalent to three in one minute. There is no "wait an hour and
   try again."
2. **The card-side check happens on the chip, not in software.** A
   wrong VERIFY is decremented atomically inside the secure element. No
   amount of host-side caching, retry, or trace recovery undoes it.
3. **Once both counters are exhausted, the card is fully sealed.** No
   reissue path short of requesting a new eGK from the Krankenkasse.

These properties are why this repo treats VERIFY as a destructive APDU.

## Why VERIFY is blocked in code

[`pkg/reader/orga/safety.go`](../../pkg/reader/orga/safety.go)
refuses any APDU with `INS=0x20` (VERIFY) on a non-CT-BCS CLA unless the
caller explicitly sets `Options.AllowPINWrite=true`. The flag exists so
that a hex-byte typo in a probe session can never burn a PIN attempt.

Equivalent CLI flag: `orga-probe -UNSAFE-allow-pin-write`.

The block also covers CT-BCS `PERFORM VERIFICATION` (`CLA=20 INS=18`),
which would trigger the terminal's PIN pad and forward a VERIFY to the
card automatically — even more dangerous because the host wouldn't see
the bytes being sent.

Refusal-list source of truth:
[`pkg/reader/orga/safety.go::dangerousISO`](../../pkg/reader/orga/safety.go).

## When will VERIFY actually be issued?

Only after the full five-phase C2C handshake completes successfully.
The sequence:

```
phase 1: Discover SMC-B CV-certs from the card
phase 2: Validate chain against gematik CVC-Root
phase 3: Push the chain to the eGK (PSO VERIFY CERTIFICATE)
phase 4: GET CHALLENGE / INT-AUTH / EXT-AUTH mutual auth
phase 5: Derive AES K_ENC + K_MAC + SSC → *sm.Session
        ↓
        only here can the host issue:
            VERIFY PIN (under SM)         <— this is the destructive APDU
            READ BINARY on EF.NFD (under SM)
            READ BINARY on EF.DPE (under SM)
            ...
```

If any earlier phase fails, the host **does not send VERIFY**. This is
the project's protection against "PIN burned for no payoff" scenarios:
if Wall 2 is unsatisfied (no live SMC-B chain), the handshake fails at
phase 3 before the PIN has even been considered.

## Practical next steps for the cardholder

If the cardholder wants to use the PIN-protected features (independent
of this project — e.g. via the official Krankenkasse apps):

1. Check whether the PIN has already been requested. Many cardholders
   have one from years ago and forgot. Look for a PIN-Brief in any
   filing of past Einschreiben mail.
2. If not, request via the Krankenkasse's app — TK-App for TK members,
   etc.
3. Complete VideoIdent / PostIdent if prompted.
4. Wait 1–2 weeks for the PIN-Brief.
5. On first PIN use, the card may force CHANGE REFERENCE DATA to swap
   the transport PIN for a personal one — follow the app's prompts.

For this project specifically, having the PIN does not by itself allow
PIN-protected reads — Wall 2 (a valid SMC-B chain) still has to be
satisfied. See [walls.md](walls.md).
