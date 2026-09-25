package store

import (
	"bytes"
	"errors"
	"fmt"
)

var ErrMergeConflict = errors.New("git merge conflict markers")

func HasConflictMarkers(raw []byte) bool {
	for _, line := range bytes.Split(raw, []byte("\n")) {
		line = bytes.TrimRight(line, "\r")
		switch {
		case bytes.HasPrefix(line, []byte("<<<<<<< ")), bytes.HasPrefix(line, []byte(">>>>>>> ")), bytes.Equal(line, []byte("=======")):
			return true
		}
	}
	return false
}

func MergeConflictError(path string) error {
	return fmt.Errorf("%w in safe spot %s: two branches superseded this safe spot and git could not merge them. "+
		"Take one side whole (git checkout --ours %s or --theirs %s, never a hand merge of the two), run the gate on the merged "+
		"backend, and if a chain or the backend changed, run the chain, propose it (shrt confirm <chain> -supersede -note \"...\") "+
		"and have a person approve it; the losing side's approval stays in git history", ErrMergeConflict, path, path, path)
}
