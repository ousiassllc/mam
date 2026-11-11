// Package keyfixture is the API keys of the tests of chatops/secretguard and of the chat that uses it: keys of the shapes of the providers that have no prefix of their own, built at
// run time (no real key is in the repository). Only tests import it.
package keyfixture

import "strings"

// ProviderKeys are keys of the providers that have no prefix of their own (the shapes secretguard finds by what every key is), by the name of the shape.
func ProviderKeys() map[string]string {
	hex := strings.Repeat("0123456789abcdef", 4)
	mixed := func(n int) string { return strings.Repeat("AbCdEf0123456789", n)[:n] }
	return map[string]string{
		"kling (32 letters and digits)": mixed(32),
		"runway (key_ + hex)":           "key" + "_" + hex[:48],
		"luma (luma- + uuid)":           "luma" + "-" + "12345678-1234-1234-1234-123456789abc",
		"minimax (JWT)":                 "ey" + "J" + mixed(30) + "." + "ey" + "J" + mixed(40) + "." + mixed(43),
		"seedance / flux (uuid)":        "12345678-1234-1234-1234-123456789abc",
		"sync labs":                     "sk" + "-" + hex[:40],
		"dotted":                        "A1b2C3d4E5f6G7h8I9j0K1l2M3n4.O5p6",
	}
}
