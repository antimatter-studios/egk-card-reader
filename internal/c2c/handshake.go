// Package c2c implements the gemSpec_COS chapter 13 card-to-card mutual
// authentication scheme. It composes the four crypto/parsing subpackages
// (cvcert, brainpool, keys, sm) into a stateful Handshake that drives
// APDUs across two Card peers (typically eGK in slot 1 and SMC-B in
// slot 2 of an ORGA terminal).
//
// Phase summary (gemSpec_COS §13 + gemSpec_PKI §6):
//
//  1. DiscoverPeerCerts    — read the SMC-B's CV-cert chain off the card.
//  2. ValidatePeerChain    — verify each link locally up to a trusted
//                            CVC-Root (gematik production or test).
//  3. PresentToVerifier    — push the SMC-B's CA + leaf cert to the eGK
//                            via MSE SET + PSO VERIFY CERTIFICATE so the
//                            eGK ends up trusting the SMC-B's public key.
//  4. MutualAuthenticate   — host-mediated GET CHALLENGE / INTERNAL & EXTERNAL
//                            AUTHENTICATE round-trip in both directions.
//  5. OpenSecureChannel    — derive AES K_ENC / K_MAC + SSC from the
//                            handshake nonces and hand off a *sm.Session.
//
// Each phase returns a typed error so callers (and tests) can distinguish
// "input data was wrong" from "the card said no" from "we don't have a
// trust anchor for this card". Phases 3-5 are scaffolded with
// ErrPhaseNotImplemented until the CVC-Root pubkey is sourced (see
// docs/c2c/plan.md and docs/c2c/cvc-root-research.md).
package c2c

import (
	"errors"
	"fmt"
	"time"

	"github.com/antimatter-studios/egk-card-reader/internal/c2c/cvcert"
	"github.com/antimatter-studios/egk-card-reader/internal/c2c/keys"
	"github.com/antimatter-studios/egk-card-reader/internal/c2c/sm"
)

// Phase enumerates the discrete stages of the C2C handshake.
type Phase int

const (
	PhaseDiscover Phase = iota + 1
	PhaseValidateChain
	PhasePresentToVerifier
	PhaseMutualAuth
	PhaseOpenSecure
)

func (p Phase) String() string {
	switch p {
	case PhaseDiscover:
		return "discover-peer-certs"
	case PhaseValidateChain:
		return "validate-peer-chain"
	case PhasePresentToVerifier:
		return "present-to-verifier"
	case PhaseMutualAuth:
		return "mutual-authenticate"
	case PhaseOpenSecure:
		return "open-secure-channel"
	}
	return fmt.Sprintf("phase-%d", int(p))
}

// Role disambiguates the two cards in a handshake.
type Role int

const (
	RoleEGK  Role = 1 // the card whose protected data we want to read
	RoleSMCB Role = 2 // the card that authenticates us to the eGK
	RoleHost Role = 3 // host-side failure (RNG, encoding, preconditions); no card involved
)

func (r Role) String() string {
	switch r {
	case RoleEGK:
		return "eGK"
	case RoleSMCB:
		return "SMC-B"
	case RoleHost:
		return "host"
	}
	return "unknown"
}

// Options configures a new Handshake. EGK and SMCB are the two card peers;
// either may be supplied by *orga.Slot, *generic.Card, or any test mock
// that satisfies the Card interface. Roots is the trusted CVC-Root list
// for chain validation — pass keys.ProductionRoots() for live eGKs, or
// keys.TestRoots() for development/test cards.
type Options struct {
	EGK   Card
	SMCB  Card
	Roots []keys.Root
	// Now is the wall-clock used for cert-validity checks. Zero = time.Now()
	// at handshake construction. Override for tests or to validate against
	// a frozen point in time.
	Now time.Time
}

// Handshake holds the running state of a C2C exchange. Construct via New;
// drive the phases in order by calling Run, or step manually for
// finer-grained control / debugging.
type Handshake struct {
	opts        Options
	now         time.Time
	smcbChain   []DiscoveredCert // SMC-B certs read in chain order (leaf → CA → ...)
	matchedRoot *keys.Root       // the Root that validated smcbChain; nil before PhaseValidateChain
	state       sessionState     // mutual-auth scratchpad; populated by phase 4
	session     *sm.Session      // populated by PhaseOpenSecure
	lastPhase   Phase
	lastErr     error
}

// New constructs a Handshake. Returns an error only on missing required
// inputs; real protocol errors surface during Run / per-phase calls.
func New(opts Options) (*Handshake, error) {
	if opts.EGK == nil {
		return nil, errors.New("c2c: EGK Card is required")
	}
	if opts.SMCB == nil {
		return nil, errors.New("c2c: SMCB Card is required")
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	return &Handshake{opts: opts, now: now}, nil
}

// Run executes phases 1-5 in order, stopping at the first error and
// returning it. Lighter-weight callers (or tests) can invoke the individual
// PhaseXxx methods directly.
func (h *Handshake) Run() error {
	for _, p := range []func() error{
		h.DiscoverPeerCerts,
		h.ValidatePeerChain,
		h.PresentToVerifier,
		h.MutualAuthenticate,
		h.OpenSecureChannel,
	} {
		if err := p(); err != nil {
			return err
		}
	}
	return nil
}

// DiscoverPeerCerts reads the SMC-B's CV-certificate(s) from the card via
// the published gemSpec FID set. On success, the chain is stored on the
// Handshake; on a per-slot read failure the error is recorded but the
// discovery continues until all candidate slots have been tried.
func (h *Handshake) DiscoverPeerCerts() error {
	h.lastPhase = PhaseDiscover
	hits, errs := DiscoverCVCerts(h.opts.SMCB, KnownSMCBCertSlots)
	if len(hits) == 0 {
		h.lastErr = &Error{
			Phase: PhaseDiscover,
			Role:  RoleSMCB,
			Msg:   "no CV-certs found in any known SMC-B slot",
			Sub:   joinErrors(errs),
		}
		return h.lastErr
	}
	h.smcbChain = hits
	return nil
}

// SMCBChain returns the SMC-B CV-certs discovered in PhaseDiscover.
// Returns nil if discovery hasn't run yet.
func (h *Handshake) SMCBChain() []DiscoveredCert { return h.smcbChain }

// ValidatePeerChain walks the discovered SMC-B chain against the configured
// roots. On success records which Root accepted the chain and returns nil.
// Expected failure modes for our test SMC-B (per project plan):
//   - chain expired (NotAfter < Now) → keys.VerifyChain returns the
//     "validity" error; we surface it as ErrChainExpired
//   - no matching root for the leaf's CAR → ErrUntrustedChain
//
// Both cases mean "stop here, document why, don't try to talk to the eGK"
// rather than soft-fail to plaintext reads.
func (h *Handshake) ValidatePeerChain() error {
	h.lastPhase = PhaseValidateChain
	if len(h.smcbChain) == 0 {
		return &Error{Phase: PhaseValidateChain, Msg: "PhaseDiscover must run first"}
	}
	if len(h.opts.Roots) == 0 {
		return &Error{Phase: PhaseValidateChain, Msg: "no CVC-Root trust anchors configured; set Options.Roots to keys.ProductionRoots() or TestRoots()"}
	}
	chain := make([]*cvcert.Cert, len(h.smcbChain))
	for i, d := range h.smcbChain {
		chain[i] = d.Cert
	}
	if err := keys.VerifyChain(chain, h.opts.Roots, h.now); err != nil {
		h.lastErr = &Error{Phase: PhaseValidateChain, Role: RoleSMCB, Cause: err, Msg: "SMC-B chain rejected by all configured roots"}
		return h.lastErr
	}
	// Record which root accepted (find by leaf's CAR matching a root Name).
	if len(chain) > 0 {
		leafCAR := chain[len(chain)-1].CAR
		for i := range h.opts.Roots {
			if h.opts.Roots[i].Name == leafCAR {
				h.matchedRoot = &h.opts.Roots[i]
				break
			}
		}
	}
	return nil
}

// MatchedRoot returns the Root that validated the SMC-B chain, or nil if
// validation has not run / failed.
func (h *Handshake) MatchedRoot() *keys.Root { return h.matchedRoot }

// PresentToVerifier pushes the SMC-B's CA + leaf CV-cert to the eGK via
// MSE SET (Manage Security Environment) + PSO VERIFY CERTIFICATE. After
// this, the eGK holds the SMC-B's public key in its volatile trust list
// and is ready to challenge it for authentication. Implementation in
// phase_present.go.
func (h *Handshake) PresentToVerifier() error {
	h.lastPhase = PhasePresentToVerifier
	h.lastErr = h.phasePresentToVerifier()
	return h.lastErr
}

// MutualAuthenticate runs the host-mediated GET CHALLENGE / INTERNAL &
// EXTERNAL AUTHENTICATE round-trip in both directions per gemSpec_COS
// chapter 13. Implementation in phase_mutual.go.
func (h *Handshake) MutualAuthenticate() error {
	h.lastPhase = PhaseMutualAuth
	h.lastErr = h.phaseMutualAuth()
	return h.lastErr
}

// OpenSecureChannel derives AES K_ENC / K_MAC / SSC from the handshake
// nonces and instantiates a *sm.Session ready for SM-protected APDUs
// (VERIFY PIN, READ BINARY of NFD/DPE/eMP). Implementation in
// phase_secure.go.
func (h *Handshake) OpenSecureChannel() error {
	h.lastPhase = PhaseOpenSecure
	h.lastErr = h.phaseOpenSecureChannel()
	return h.lastErr
}

// sessionState is the mutual-auth scratchpad populated by phase 4 and
// consumed by phase 5. Internal; exposed only via Session().
type sessionState struct {
	// nonceHost is the random the host generated and fed into the
	// SMC-B's INTERNAL AUTHENTICATE (forms part of K_ENC / K_MAC input).
	nonceHost []byte
	// nonceCard is the random returned by the eGK's GET CHALLENGE
	// (forms part of K_ENC / K_MAC input).
	nonceCard []byte
	// negotiatedAlg names the gemSpec_Krypt algorithm both cards agreed
	// on; determines key sizes in phase 5. Examples: "AES-128", "AES-256".
	negotiatedAlg string
}

// Session returns the SM session opened by OpenSecureChannel, or nil if
// the handshake hasn't reached that phase. Use *Session.Wrap / .Unwrap to
// send protected APDUs to the eGK.
func (h *Handshake) Session() *sm.Session { return h.session }

// LastPhase / LastError report the last phase attempted and the error it
// produced (nil on success). Useful for diagnostic output.
func (h *Handshake) LastPhase() Phase { return h.lastPhase }
func (h *Handshake) LastError() error { return h.lastErr }

// Error is the typed error returned by every Handshake phase.
type Error struct {
	Phase Phase
	Role  Role  // 0 = N/A
	Cause error // wrapped lower-level error, e.g. from keys.VerifyChain
	Sub   error // joined per-slot errors during discovery; nil otherwise
	Msg   string
}

func (e *Error) Error() string {
	parts := []string{e.Phase.String()}
	if e.Role != 0 {
		parts = append(parts, "role="+e.Role.String())
	}
	if e.Msg != "" {
		parts = append(parts, e.Msg)
	}
	if e.Cause != nil {
		parts = append(parts, "cause: "+e.Cause.Error())
	}
	if e.Sub != nil {
		parts = append(parts, "sub: "+e.Sub.Error())
	}
	return "c2c: " + joinStrings(parts, ": ")
}
func (e *Error) Unwrap() error { return e.Cause }

func joinErrors(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

func joinStrings(parts []string, sep string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += sep
		}
		out += p
	}
	return out
}
