package diff

import (
	"sort"
	"strings"

	"github.com/N4darae/shrt/pathmask"
)

const minRenamedID = 4

type comparedStep struct {
	id        string
	want, got any
	sent      any
	mask      *pathmask.Masker
}

func idRenames(pairs []idPair) [][2]string {
	forward := map[string]string{}
	reverse := map[string]string{}
	order := []string{}
	for _, p := range pairs {
		w, okW := p.want.(string)
		g, okG := p.got.(string)
		if !okW || !okG {
			continue
		}
		if _, ok := forward[w]; ok {
			continue
		}
		if _, ok := reverse[g]; ok {
			continue
		}
		forward[w], reverse[g] = g, w
		order = append(order, w)
	}
	out := [][2]string{}
	for _, w := range order {
		if g := forward[w]; w != g && len(w) >= minRenamedID {
			out = append(out, [2]string{w, g})
		}
	}
	return out
}

func renamer(pairs [][2]string) *strings.Replacer {
	seen := map[string]string{}
	conflict := map[string]bool{}
	for _, p := range pairs {
		if p[0] == p[1] || p[0] == "" {
			continue
		}
		if prev, ok := seen[p[0]]; ok && prev != p[1] {
			conflict[p[0]] = true
			continue
		}
		seen[p[0]] = p[1]
	}
	olds := make([]string, 0, len(seen))
	for o := range seen {
		if !conflict[o] {
			olds = append(olds, o)
		}
	}
	if len(olds) == 0 {
		return nil
	}
	sort.Slice(olds, func(i, j int) bool {
		if len(olds[i]) != len(olds[j]) {
			return len(olds[i]) > len(olds[j])
		}
		return olds[i] < olds[j]
	})
	args := make([]string, 0, 2*len(olds))
	for _, o := range olds {
		args = append(args, o, seen[o])
	}
	return strings.NewReplacer(args...)
}

func walkRenamedText(steps []comparedStep, r *strings.Replacer, visit func(step, path, want, got, renamed string)) {
	if r == nil {
		return
	}
	for _, st := range steps {
		prune := func(path string) bool { return underMask(st.mask, path) }
		walkShared(st.want, st.got, "", prune, func(path string, want, got any) {
			w, okW := want.(string)
			g, okG := got.(string)
			if !okW || !okG {
				return
			}
			if renamed := r.Replace(w); renamed != w && !renameable(path, w, g) {
				visit(st.id, pathOr(path), w, g, renamed)
			}
		})
	}
}

func splitStaleEchoes(changes []Change, steps []comparedStep, pairs [][2]string) (kept, echoed, stale []Change) {
	echo := map[string]bool{}
	walkRenamedText(steps, renamer(pairs), func(step, path, want, got, renamed string) {
		switch {
		case want != got && renamed == got:
			echo[step+" "+path] = true
		case want == got:
			stale = append(stale, Change{Step: step, Path: path, Kind: KindChanged, Want: renamed, Got: got,
				Detail: "still the confirmed run's value although this run sent another fixture name or got another id; an echo of this run's input reads as want"})
		}
	})
	if len(echo) == 0 {
		return changes, nil, stale
	}
	kept = changes[:0:0]
	for _, c := range changes {
		if c.Kind == KindChanged && echo[c.Step+" "+c.Path] {
			echoed = append(echoed, c)
			continue
		}
		kept = append(kept, c)
	}
	return kept, echoed, stale
}
