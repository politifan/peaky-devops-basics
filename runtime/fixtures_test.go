package main

import (
	"strings"
	"testing"
)

func TestFixtureSeedReproducibleAndVaries(t *testing.T) {
	a, b, c, d, e := fixtureTexts(417)
	x, y, z, u, v := fixtureTexts(417)
	if a != x || b != y || c != z || d != u || e != v {
		t.Fatal("same seed must be identical")
	}
	if a != "Заявка T417 не сохраняется.\n" || e != "E_DB\nE_IO\n" {
		t.Fatal("default tutorial fixture changed")
	}
	f, g, h, j, k := fixtureTexts(418)
	if a == f || b == g || c == h || d == j || e == k {
		t.Fatal("other seed must change actual fixtures")
	}
	if !strings.Contains(h, "ticket=T418") || k != "E_DNS\nE_IO\n" {
		t.Fatal("variant does not match announced ticket")
	}
	for _, seed := range []int{-1, 0, 1000000} {
		a, _, _, _, _ := fixtureTexts(seed)
		if a != "" {
			t.Fatal("invalid seed accepted")
		}
	}
}
