package sm

import (
	"bytes"
	"crypto/aes"
	"encoding/hex"
	"testing"
)

// NIST SP 800-38B, Appendix D — Examples 1 (AES-128) and 2 (AES-256).
// https://csrc.nist.gov/publications/detail/sp/800-38b/final
//
// Example 1 (AES-128)
//   K  = 2b7e1516 28aed2a6 abf71588 09cf4f3c
//   M  = (empty)            T = bb1d6929 e9593728 7fa37d12 9b756746
//   M  = 16 bytes (one block)
//                          T = 070a16b4 6b4d4144 f79bdd9d d04a287c
//   M  = 40 bytes (two and a half blocks)
//                          T = dfa66747 de9ae630 30ca3261 1497c827
//   M  = 64 bytes (four blocks)
//                          T = 51f0bebf 7e3b9d92 fc497417 79363cfe
//
// Example 2 (AES-256)
//   K  = 603deb10 15ca71be 2b73aef0 857d7781
//        1f352c07 3b6108d7 2d9810a3 0914dff4
//   Same four messages, different tags:
//     empty: 028962f6 1b7bf89e fc6b551f 4667d983
//     16   : 28a7023f 452e8f82 bd4bf28d 8c37c35c
//     40   : aaf3d8f1 de5640c2 32f5b169 b9c911e6
//     64   : e1992190 549f6ed5 696a2c05 6c315410

func TestCMAC_NIST_AES128(t *testing.T) {
	t.Parallel()
	key, _ := hex.DecodeString("2b7e151628aed2a6abf7158809cf4f3c")
	m16, _ := hex.DecodeString("6bc1bee22e409f96e93d7e117393172a")
	m40, _ := hex.DecodeString("" +
		"6bc1bee22e409f96e93d7e117393172a" +
		"ae2d8a571e03ac9c9eb76fac45af8e51" +
		"30c81c46a35ce411")
	m64, _ := hex.DecodeString("" +
		"6bc1bee22e409f96e93d7e117393172a" +
		"ae2d8a571e03ac9c9eb76fac45af8e51" +
		"30c81c46a35ce411e5fbc1191a0a52ef" +
		"f69f2445df4f9b17ad2b417be66c3710")

	tests := []struct {
		name string
		msg  []byte
		want string
	}{
		{"empty", nil, "bb1d6929e95937287fa37d129b756746"},
		{"len16", m16, "070a16b46b4d4144f79bdd9dd04a287c"},
		{"len40", m40, "dfa66747de9ae63030ca32611497c827"},
		{"len64", m64, "51f0bebf7e3b9d92fc49741779363cfe"},
	}
	c, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := cmac(c, tc.msg)
			want, _ := hex.DecodeString(tc.want)
			if !bytes.Equal(got, want) {
				t.Fatalf("CMAC mismatch:\n got=%x\nwant=%x", got, want)
			}
		})
	}
}

func TestCMAC_NIST_AES256(t *testing.T) {
	t.Parallel()
	key, _ := hex.DecodeString("" +
		"603deb1015ca71be2b73aef0857d7781" +
		"1f352c073b6108d72d9810a30914dff4")
	m16, _ := hex.DecodeString("6bc1bee22e409f96e93d7e117393172a")
	m40, _ := hex.DecodeString("" +
		"6bc1bee22e409f96e93d7e117393172a" +
		"ae2d8a571e03ac9c9eb76fac45af8e51" +
		"30c81c46a35ce411")
	m64, _ := hex.DecodeString("" +
		"6bc1bee22e409f96e93d7e117393172a" +
		"ae2d8a571e03ac9c9eb76fac45af8e51" +
		"30c81c46a35ce411e5fbc1191a0a52ef" +
		"f69f2445df4f9b17ad2b417be66c3710")

	tests := []struct {
		name string
		msg  []byte
		want string
	}{
		{"empty", nil, "028962f61b7bf89efc6b551f4667d983"},
		{"len16", m16, "28a7023f452e8f82bd4bf28d8c37c35c"},
		{"len40", m40, "aaf3d8f1de5640c232f5b169b9c911e6"},
		{"len64", m64, "e1992190549f6ed5696a2c056c315410"},
	}
	c, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := cmac(c, tc.msg)
			want, _ := hex.DecodeString(tc.want)
			if !bytes.Equal(got, want) {
				t.Fatalf("CMAC mismatch:\n got=%x\nwant=%x", got, want)
			}
		})
	}
}

// TestCMAC_SubkeyCache verifies cmacWithSubkeys matches cmac across a
// range of message lengths (zero, partial blocks, full blocks).
func TestCMAC_SubkeyCache(t *testing.T) {
	t.Parallel()
	key, _ := hex.DecodeString("2b7e151628aed2a6abf7158809cf4f3c")
	c, _ := aes.NewCipher(key)
	k1, k2 := cmacSubkeys(c)

	for i := 0; i < 200; i++ {
		msg := make([]byte, i)
		for j := range msg {
			msg[j] = byte(j * 7)
		}
		a := cmac(c, msg)
		b := cmacWithSubkeys(c, msg, k1, k2)
		if !bytes.Equal(a, b) {
			t.Fatalf("len=%d mismatch:\n a=%x\n b=%x", i, a, b)
		}
	}
}

// TestCMAC_SubkeyValues spot-checks the K1/K2 derivation against the
// values that NIST SP 800-38B Appendix D documents for the AES-128 key.
//
//	K1 = fbeed618 35713366 7c85e08f 7236a8de
//	K2 = f7ddac30 6ae266cc f90bc11e e46d513b
func TestCMAC_SubkeyValues_AES128(t *testing.T) {
	t.Parallel()
	key, _ := hex.DecodeString("2b7e151628aed2a6abf7158809cf4f3c")
	c, _ := aes.NewCipher(key)
	k1, k2 := cmacSubkeys(c)
	wantK1, _ := hex.DecodeString("fbeed618357133667c85e08f7236a8de")
	wantK2, _ := hex.DecodeString("f7ddac306ae266ccf90bc11ee46d513b")
	if !bytes.Equal(k1, wantK1) {
		t.Fatalf("K1 mismatch:\n got=%x\nwant=%x", k1, wantK1)
	}
	if !bytes.Equal(k2, wantK2) {
		t.Fatalf("K2 mismatch:\n got=%x\nwant=%x", k2, wantK2)
	}
}
