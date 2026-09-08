package contract

import (
	"fmt"
	"sort"
)

type PendingDeployEntry struct {
	Domain string `json:"domain"`
	RPC    string `json:"rpc"`
	Code   int    `json:"code,omitempty"`
	Reason string `json:"reason,omitempty"`
	Commit string `json:"commit"`
	When   string `json:"when,omitempty"`
}

func PendingDeploy(l *Library) []PendingDeployEntry {
	out := []PendingDeployEntry{}
	if l == nil {
		return out
	}
	for _, rpc := range l.RPCs() {
		for _, f := range l.AllFailures(rpc) {
			if f.PendingDeploy == "" {
				continue
			}
			out = append(out, PendingDeployEntry{
				Domain: l.Domain(rpc),
				RPC:    rpc,
				Code:   f.Code,
				Reason: f.Reason,
				Commit: f.PendingDeploy,
				When:   f.When,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].RPC != out[j].RPC {
			return out[i].RPC < out[j].RPC
		}
		return out[i].Code < out[j].Code
	})
	return out
}

func (e PendingDeployEntry) Label() string {
	switch {
	case e.Code != 0 && e.Reason != "":
		return fmt.Sprintf("%d %s", e.Code, e.Reason)
	case e.Code != 0:
		return fmt.Sprintf("%d", e.Code)
	default:
		return e.Reason
	}
}
