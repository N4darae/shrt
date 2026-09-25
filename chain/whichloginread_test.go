package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestWhichReproducesALoginAsARead(t *testing.T) {
	c := &chain.Chain{Name: "auth", Steps: []*chain.Step{
		{ID: "login_admin", Call: "pkg.AuthService/Login", SkipAuth: true, Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}}},
	}}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	obs := func(string) []chain.Observation {
		return []chain.Observation{{Run: "r1", Step: "login_admin", Status: "passed", Reached: true, Response: okResponse()}}
	}
	hits := chain.Which([]*chain.Chain{c}, chain.WhichQuery{RPC: "pkg.AuthService/Login"}, chain.WhichOptions{Observations: obs})
	if !strings.Contains(hits[0].Command, "-keep writes") {
		t.Fatalf("without being told it is the login, Login is a write by its name: %q", hits[0].Command)
	}
	hits = chain.Which([]*chain.Chain{c}, chain.WhichQuery{RPC: "pkg.AuthService/Login"}, chain.WhichOptions{
		Observations: obs,
		ReadsOnly:    func(s *chain.Step) bool { return s.Call == "pkg.AuthService/Login" },
	})
	if strings.Contains(hits[0].Command, "-keep writes") || !strings.Contains(hits[0].Command, "-mode pin -run r1") {
		t.Fatalf("the auth login changes nothing a slice depends on, so it is reproduced as a read: %q", hits[0].Command)
	}
}
