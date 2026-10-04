package runner

import (
	"context"
	"net/http"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/transport"
)

func (r *Runner) remeasure(ctx context.Context, step *chain.Step, procedure string, body []byte, header http.Header, opts Options, first int64) []int64 {
	if opts.LatencySuspect == nil || opts.Remeasure <= 0 || !chain.IsReadOnlyCall(step.Call) ||
		step.Auth == config.InvalidTokenProfile || !opts.LatencySuspect(step.ID, first) {
		return nil
	}
	var out []int64
	for i := 0; i < opts.Remeasure; i++ {
		again := &transport.Call{Procedure: procedure, Body: body, Header: header.Clone(), Meta: authMeta(step)}
		res, err := r.Client.Do(ctx, again)
		if err != nil || res.Error != nil {
			break
		}
		ms := res.Latency.Milliseconds()
		out = append(out, ms)
		if !opts.LatencySuspect(step.ID, ms) {
			break
		}
	}
	return out
}
