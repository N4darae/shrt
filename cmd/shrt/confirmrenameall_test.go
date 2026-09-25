package main

import (
	"strings"
	"testing"
)

func TestConfirmRenameFromNamesEveryMissingAndExtraStep(t *testing.T) {
	renameThingFlow(t, func(s string) string {
		s = strings.Replace(s, "    - id: fetch\n", "    - id: fetch_again\n", 1)
		return s
	})
	_, err := confirmRename(t, "-by", "bob@example.test")
	if err == nil {
		t.Fatal("a renamed step is not a pure chain rename")
	}
	for _, want := range []string{"2 place(s)", "fetch step", "fetch_again step"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal lists every missing and extra step, want %q in:\n%v", want, err)
		}
	}
}
