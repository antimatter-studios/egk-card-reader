package egk

// Card is the subset of *scard.Card that internal/egk needs. Defining it here
// lets tests stub APDU exchanges without a live PC/SC reader; *scard.Card from
// github.com/ebfe/scard satisfies the interface automatically.
type Card interface {
	Transmit([]byte) ([]byte, error)
}
