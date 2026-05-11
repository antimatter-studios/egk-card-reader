package egk

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"testing"
)

// buildFuzzSeedPD mirrors buildPD from parse_test.go but takes *testing.F so
// it can be used to construct seed corpus bytes inside a fuzz target.
func buildFuzzSeedPD(f *testing.F, xmlBody string) []byte {
	f.Helper()
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	if _, err := zw.Write([]byte(xmlBody)); err != nil {
		f.Fatal(err)
	}
	zw.Close()

	out := make([]byte, 2+gz.Len())
	binary.BigEndian.PutUint16(out[:2], uint16(gz.Len()))
	copy(out[2:], gz.Bytes())
	return out
}

// buildFuzzSeedVD mirrors buildVD from parse_test.go for *testing.F.
func buildFuzzSeedVD(f *testing.F, avdXML, gvdXML string) []byte {
	f.Helper()

	var avdGz bytes.Buffer
	zw := gzip.NewWriter(&avdGz)
	zw.Write([]byte(avdXML))
	zw.Close()

	header := make([]byte, 8)
	avdStart := uint16(8)
	avdEnd := avdStart + uint16(avdGz.Len()) - 1
	binary.BigEndian.PutUint16(header[0:2], avdStart)
	binary.BigEndian.PutUint16(header[2:4], avdEnd)

	out := append([]byte{}, header...)
	out = append(out, avdGz.Bytes()...)

	if gvdXML == "" {
		return out
	}

	var gvdGz bytes.Buffer
	zw2 := gzip.NewWriter(&gvdGz)
	zw2.Write([]byte(gvdXML))
	zw2.Close()

	gvdStart := uint16(len(out))
	gvdEnd := gvdStart + uint16(gvdGz.Len()) - 1
	binary.BigEndian.PutUint16(out[4:6], gvdStart)
	binary.BigEndian.PutUint16(out[6:8], gvdEnd)
	out = append(out, gvdGz.Bytes()...)
	return out
}

func FuzzParsePD(f *testing.F) {
	f.Add(buildFuzzSeedPD(f, samplePDXML))
	f.Add([]byte(""))
	f.Add([]byte{0x00})
	f.Add([]byte{0x00, 0x04, 'a', 'b', 'c', 'd'})
	f.Add([]byte{0xFF, 0xFF, 0x00})

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _, _ = ParsePD(data)
	})
}

func FuzzParseVD(f *testing.F) {
	f.Add(buildFuzzSeedVD(f, sampleAVDXML, sampleGVDXML))
	f.Add(buildFuzzSeedVD(f, sampleAVDXML, ""))
	f.Add([]byte(""))
	f.Add(make([]byte, 4))
	f.Add(make([]byte, 8))

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _, _, _, _ = ParseVD(data)
	})
}
