package fleet

import (
	"crypto/sha256"
	"encoding/base32"
	"fmt"
	"strings"
)

const deviceAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"

var deviceEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// DeviceID encodes a certificate fingerprint as eight groups of seven characters.
// Each pair of display groups contains 13 data characters and a Luhn-32 check
// character (52 base32 data characters and four checks in total).
func DeviceID(der []byte) string {
	fp := sha256.Sum256(der)
	raw := deviceEncoding.EncodeToString(fp[:])
	groups := make([]string, 0, 8)
	for i := 0; i < len(raw); i += 13 {
		block := raw[i : i+13]
		block += string(deviceCheck(block))
		groups = append(groups, block[:7], block[7:])
	}
	return strings.Join(groups, "-")
}

// ParseDeviceID validates the layout, checksums and canonical base32 encoding.
// Lowercase input is accepted for manual entry.
func ParseDeviceID(id string) ([32]byte, error) {
	var fp [32]byte
	groups := strings.Split(strings.ToUpper(id), "-")
	if len(groups) != 8 {
		return fp, fmt.Errorf("device id must have eight groups of seven characters")
	}
	for _, group := range groups {
		if len(group) != 7 {
			return fp, fmt.Errorf("device id must have eight groups of seven characters")
		}
		for i := range group {
			if strings.IndexByte(deviceAlphabet, group[i]) < 0 {
				return fp, fmt.Errorf("invalid device id character")
			}
		}
	}
	raw := ""
	for i := 0; i < len(groups); i += 2 {
		block := groups[i] + groups[i+1]
		if deviceCheck(block[:13]) != block[13] {
			return fp, fmt.Errorf("device id check digit mismatch")
		}
		raw += block[:13]
	}
	b, err := deviceEncoding.DecodeString(raw)
	if err != nil || len(b) != len(fp) || deviceEncoding.EncodeToString(b) != raw {
		return fp, fmt.Errorf("invalid device id fingerprint encoding")
	}
	copy(fp[:], b)
	return fp, nil
}

func deviceCheck(block string) byte {
	sum, factor := 0, 2
	for i := len(block) - 1; i >= 0; i-- {
		v := factor * strings.IndexByte(deviceAlphabet, block[i])
		sum += v/32 + v%32
		factor = 3 - factor
	}
	return deviceAlphabet[(32-sum%32)%32]
}
