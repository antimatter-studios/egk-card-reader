package ktda

import (
	"bytes"
	"testing"
)

func FuzzParseKE0(f *testing.F) {
	f.Add([]byte("UNH+1'IDK+1++X'UNT+2+1'"))
	f.Add([]byte("UNH+1+KOTRDA:14:0:0'IDK+109519005+02+TK+12345'VDT+20240101+20251231'NAM+1+Techniker+Krankenkasse'VKG+01+109519005+5+109519005'UNT+5+1'"))
	f.Add([]byte(""))
	f.Add([]byte("'"))
	f.Add([]byte("AB'"))
	f.Add([]byte("UNH+1'\r\nIDK+1++X'\r\nUNT+2+1'\r\n"))
	f.Add([]byte("IDK+1++Name?+with?+pluses'"))

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = Parse(bytes.NewReader(data), "EK")
	})
}
