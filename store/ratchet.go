package store

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type RatchetVerdict int

const (
	RatchetAtBaseline RatchetVerdict = iota
	RatchetWorse
	RatchetBetter
)

func ReadBaseline(path string) (int, error) {
	if path == "" {
		return 0, fmt.Errorf("a ratchet needs a baseline file, the file holding the count to hold to")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(raw))
	if len(fields) == 0 {
		return 0, fmt.Errorf("%s is empty, expected a single number", path)
	}
	want, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, fmt.Errorf("%s: %q is not a number", path, fields[0])
	}
	return want, nil
}

func Ratchet(path string, have int) (int, RatchetVerdict, error) {
	want, err := ReadBaseline(path)
	if err != nil {
		return 0, RatchetAtBaseline, err
	}
	switch {
	case have > want:
		return want, RatchetWorse, nil
	case have < want:
		return want, RatchetBetter, nil
	}
	return want, RatchetAtBaseline, nil
}
