package main

import "fmt"

// The announced seed selects fixture content; expected values are computed by
// the checker from its assigned seed, never loaded from student-written answers.
func fixtureTexts(seed int) (original, log, selection, codes, unique string) {
	if seed < 1 || seed > 999999 {
		return "", "", "", "", ""
	}
	original = fmt.Sprintf("Заявка T%d не сохраняется.\n", seed)
	selection = fmt.Sprintf("2026-10-04T10:00:01Z ERROR ticket=T%d write_failed\n2026-10-04T10:00:02Z INFO ticket=T%d retry_scheduled\n", seed, seed)
	log = fmt.Sprintf("2026-10-04T10:00:00Z INFO ticket=T%d created\n", seed-1) + selection + fmt.Sprintf("2026-10-04T10:00:03Z WARN ticket=T%d slow\n", seed+1)
	code := "E_DB"
	if seed%2 == 0 {
		code = "E_DNS"
	}
	codes = "E_IO\n" + code + "\nE_IO\n" + code + "\n"
	unique = code + "\nE_IO\n"
	return
}
