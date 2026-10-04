package runner_test

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/runner"
)

func memberConfig(extra ...string) func(string) *config.Config {
	return func(url string) *config.Config {
		cfg := testConfig(url)
		cfg.Auth.Profiles = map[string]*config.Auth{"member": {Call: "PartnerAuthService/Login", Body: map[string]any{"username": "alice", "password": "alice-pin"}}}
		for _, name := range extra {
			cfg.Auth.Profiles[name] = &config.Auth{Call: "PartnerAuthService/Login", Body: map[string]any{"username": name, "password": name + "-pin"}}
		}
		return cfg
	}
}

func checkerConfig(url string) *config.Config {
	cfg := testConfig(url)
	cfg.Auth.Profiles = map[string]*config.Auth{"checker": {Call: "AuthService/Login", Body: map[string]any{"username": "checker", "password": "secret"},
		TokenPath: "access_token", ExpiresPath: "expires_at"}}
	return cfg
}

func login(id, call, user, password, auth string) *chain.Step {
	s := step(id, call, map[string]any{"username": user, "password": password}, okExpect()...)
	s.SkipAuth = auth == "-"
	if !s.SkipAuth {
		s.Auth = auth
	}
	return s
}

func TestEachStepCarriesTheTokenOfTheProfileItRunsAs(t *testing.T) {
	create := step("create", "ThingService/Create", thing("widget"), okExpect()...)
	mine := func(auth string) *chain.Step {
		s := step("mine", "PartnerService/FetchMine", byID("p-1"), okExpect()...)
		s.Auth = auth
		return s
	}
	partnerCalls := func(url string) *config.Config {
		cfg := partnerConfig(url)
		cfg.Auth.Profiles["partner"].Calls = []string{"shrt.test.v1.PartnerService/*"}
		return cfg
	}
	for _, tc := range []struct {
		name            string
		cfg             func(string) *config.Config
		c               *chain.Chain
		note            [2]string
		noteNot         []string
		carries         [2]string
		logins, partner int
		profiles        map[string]string
	}{
		{name: "a step asks for a named profile", cfg: partnerConfig, c: flow(create, mine("partner")), carries: [2]string{"PartnerService/FetchMine", "partner:partner@example.com"}, logins: 1, partner: 1},
		{name: "a profile claims its own procedures", cfg: partnerCalls, c: flow(mine("")), carries: [2]string{"PartnerService/FetchMine", "partner:partner@example.com"}, logins: 0, partner: 1},
		{name: "the default profile still drives every other call", cfg: partnerConfig, c: testChain(), carries: [2]string{"ThingService/Create", "staff"}, logins: 1, partner: 0},
		{name: "an in-chain partner login seeds its own profile", cfg: partnerConfig, c: flow(login("partner_login", "PartnerAuthService/Login", "partner@example.com", "portal", ""), mine("partner")),
			note: [2]string{"partner_login", "partner"}, logins: 0, partner: 1},
		{name: "an explicit login seeds the shared token", cfg: testConfig, c: flow(login("login", "AuthService/Login", "staff", "secret", "-"), create),
			note: [2]string{"login", "seeded"}, carries: [2]string{"ThingService/Create", "staff"}, logins: 1, partner: 0},
		{name: "a login with other credentials does not seed the only profile on the rpc", cfg: memberConfig(), c: flow(login("login_other", "PartnerAuthService/Login", "bob", "bob-pin", "-"), mine("member")),
			note: [2]string{"login_other", "did not seed"}, carries: [2]string{"PartnerService/FetchMine", "partner:alice"}, logins: 0, partner: 2},
		{name: "a login with other credentials among several profiles", cfg: memberConfig("carol"), c: flow(login("login_other", "PartnerAuthService/Login", "bob", "bob-pin", "-"), mine("member")),
			note: [2]string{"login_other", "member"}, carries: [2]string{"PartnerService/FetchMine", "partner:alice"}, logins: 0, partner: 2},
		{name: "a login with a profile's own credentials seeds it", cfg: memberConfig(), c: flow(login("login_other", "PartnerAuthService/Login", "alice", "alice-pin", "-"), mine("member")),
			note: [2]string{"login_other", "seeded the member"}, logins: 0, partner: 1},
		{name: "a login with a profile's own credentials among several", cfg: memberConfig("carol"), c: flow(login("login_other", "PartnerAuthService/Login", "alice", "alice-pin", "-"), mine("member")),
			note: [2]string{"login_other", "seeded the member"}, logins: 0, partner: 1},
		{name: "a login as someone else does not seed the profile the step ran as", cfg: checkerConfig, c: flow(login("login_subject", "AuthService/Login", "subject", "secret", ""), create),
			note: [2]string{"login_subject", "did not seed"}, carries: [2]string{"ThingService/Create", "staff"}, logins: 2},
		{name: "a login under a profile seeds that profile, not the default", cfg: checkerConfig, c: flow(login("login_checker", "AuthService/Login", "checker", "secret", "checker"), create),
			carries: [2]string{"ThingService/Create", "staff"}, logins: 3},
		{name: "a login seeding one profile does not claim it failed another", cfg: checkerConfig, c: flow(login("checker_login", "AuthService/Login", "checker", "secret", "-")),
			note: [2]string{"checker_login", "seeded the checker auth token"}, noteNot: []string{"did not seed", "declare a profile"}, logins: 1},
		{name: "a login matching no profile names every profile once", cfg: checkerConfig, c: flow(login("stranger_login", "AuthService/Login", "subject", "secret", "-")),
			note: [2]string{"stranger_login", "every profile it could seed (checker, shared (default))"}, logins: 1},
		{name: "each step records the profile it ran under", cfg: memberConfig(), c: flow(login("login_other", "PartnerAuthService/Login", "bob", "bob-pin", "-"), create, mine("member")),
			profiles: map[string]string{"login_other": "none", "create": "default", "mine": "member"}, logins: 1, partner: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeServer()
			t.Cleanup(f.Close)
			rec := run(t, buildRunner(t, tc.cfg(f.URL), catalogtest.New()), normalized(t, tc.c), runner.Options{})
			if !rec.Passed() {
				t.Fatalf("want passed, got %s: %s", rec.Status, rec.Failure)
			}
			if f.loginCount() != tc.logins || f.partnerLoginCount() != tc.partner {
				t.Errorf("logins %d partner %d, want %d %d", f.loginCount(), f.partnerLoginCount(), tc.logins, tc.partner)
			}
			if tc.note[0] != "" {
				note := stepByID(t, rec, tc.note[0]).Note
				if !strings.Contains(note, tc.note[1]) || strings.Count(note, "did not seed") > 1 || strings.HasSuffix(strings.TrimSpace(note), "auth:") {
					t.Errorf("note %q, want it to say %q in one sentence", note, tc.note[1])
				}
				for _, s := range tc.noteNot {
					if strings.Contains(note, s) {
						t.Errorf("note %q must not say %q", note, s)
					}
				}
			}
			if tc.carries[0] != "" {
				user, partner := strings.CutPrefix(tc.carries[1], "partner:")
				want := f.tokenIssuedTo
				if partner {
					want = f.partnerTokenIssuedTo
				}
				if got := f.headerFor("/shrt.test.v1."+tc.carries[0], "Authorization"); got != "Bearer "+want(t, user) {
					t.Errorf("%s carried %q, want %s's token", tc.carries[0], got, tc.carries[1])
				}
			}
			for id, profile := range tc.profiles {
				if got := stepByID(t, rec, id).AuthProfile; got != profile {
					t.Errorf("step %s auth_profile = %q, want %q", id, got, profile)
				}
			}
		})
	}
}

func TestSeededTokenStillRefreshesWhenRejected(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	r := newRunner(t, srv)
	srv.expireTokens()
	c := flow(login("login", "AuthService/Login", "staff", "secret", "-"), step("fetch", "ThingService/Fetch", byID("t-1"), okExpect()...))
	if rec := run(t, r, normalized(t, c), runner.Options{}); !rec.Passed() || srv.loginCount() != 2 {
		t.Fatalf("a seeded token the server rejects still refreshes: %s, %d logins", rec.Failure, srv.loginCount())
	}
}

func TestAProfileIsNotConfusedWithSkipAuth(t *testing.T) {
	c := normalized(t, flow(&chain.Step{ID: "mine", Call: "PartnerService/FetchMine", Auth: "partner", SkipAuth: true, Body: byID("p-1")}))
	for _, i := range chain.Lint(c, catalogtest.New()) {
		if i.Severity == chain.SeverityError && strings.Contains(i.Message, "skip_auth") {
			return
		}
	}
	t.Fatal("skip_auth plus auth must be reported as a contradiction")
}

func TestAuthCoverageAnswersWhatTheMiddlewareWouldDo(t *testing.T) {
	cfg := testConfig("http://unused")
	cfg.Auth.Profiles = map[string]*config.Auth{"partner": {Call: "PartnerAuthService/Login", Header: "X-Api-Key", Calls: []string{"shrt.test.v1.PartnerService/*"},
		Body: map[string]any{"username": "${env.PARTNER_USER}", "password": "${env.PARTNER_PASSWORD}"}}}
	covers, err := runner.AuthCoverage(cfg, catalogtest.New())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		step            *chain.Step
		profile, header string
		covered         bool
	}{
		{&chain.Step{Call: "ThingService/Create"}, "default", "Authorization", true},
		{&chain.Step{Call: "PartnerService/FetchMine"}, "partner", "X-Api-Key", true},
		{&chain.Step{Call: "ThingService/Fetch", Auth: "partner"}, "partner", "X-Api-Key", true},
		{&chain.Step{Call: "ThingService/Fetch", SkipAuth: true}, "", "", false},
		{&chain.Step{Call: "AuthService/Login"}, "", "", false},
	} {
		if profile, header, covered := covers(c.step); profile != c.profile || header != c.header || covered != c.covered {
			t.Errorf("%+v: got (%q, %q, %v)", c.step, profile, header, covered)
		}
	}
	cfg.Auth = nil
	covers, err = runner.AuthCoverage(cfg, catalogtest.New())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, covered := covers(&chain.Step{Call: "ThingService/Create"}); covered {
		t.Fatal("with no auth block nothing overwrites a header")
	}
}

func principalOf(t *testing.T, user, password string, redact ...string) (string, string) {
	t.Helper()
	srv := newFakeServer()
	defer srv.Close()
	t.Setenv("PRINCIPAL_USER", user)
	t.Setenv("PRINCIPAL_PASSWORD", password)
	cfg := testConfig(srv.URL)
	cfg.Auth.Body = map[string]any{"username": "${env.PRINCIPAL_USER}", "password": "${env.PRINCIPAL_PASSWORD}"}
	cfg.Redact = append(append([]string{}, cfg.Redact...), redact...)
	rec := run(t, buildRunner(t, cfg, catalogtest.New()), normalized(t, flow(step("create", "ThingService/Create", thing("w"), okExpect()...))), runner.Options{Redact: cfg.Redact})
	return rec.Steps[0].AuthPrincipal, recordText(t, rec)
}

func TestEachStepRecordsWhichPrincipalItsProfileLoggedInAs(t *testing.T) {
	admin, raw := principalOf(t, "admin-user", "pw-one-111")
	if admin == "" || strings.Contains(raw, "pw-one-111") {
		t.Fatalf("a step records its principal, without the secret: %s", raw)
	}
	if again, _ := principalOf(t, "admin-user", "pw-two-222"); again != admin {
		t.Fatalf("a rotated password is the same principal, got %q and %q", admin, again)
	}
	if clerk, _ := principalOf(t, "clerk-user", "pw-one-111"); clerk == admin {
		t.Fatalf("another username is another principal")
	}
	adminRedacted, _ := principalOf(t, "admin-user", "pw-one-111", "**.username")
	clerkRedacted, _ := principalOf(t, "clerk-user", "pw-one-111", "**.username")
	if adminRedacted != admin || clerkRedacted == adminRedacted {
		t.Fatalf("the principal does not depend on the redact list: %q %q %q", admin, adminRedacted, clerkRedacted)
	}
}

func tokenServer(t *testing.T, f *fakeServer, expiring bool, armed *atomic.Bool, serve func(w http.ResponseWriter, r *http.Request, next http.HandlerFunc)) string {
	t.Helper()
	srv := wrap(f, expiring, func(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
		if armed.Load() {
			serve(w, r, next)
			return
		}
		next(w, r)
	})
	t.Cleanup(srv.Close)
	return srv.URL
}

func firstUnauth(f *fakeServer, rpc string) func(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	var done atomic.Bool
	return func(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
		if strings.HasSuffix(r.URL.Path, "/"+rpc) && done.CompareAndSwap(false, true) {
			f.handle(&bufferWriter{h: http.Header{}}, r)
			unauth(w, "token expired")
			return
		}
		next(w, r)
	}
}

func alwaysUnauth(rpc string) func(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	return func(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
		if strings.HasSuffix(r.URL.Path, "/"+rpc) {
			unauth(w, "token rejected")
			return
		}
		next(w, r)
	}
}

func forgetOnSecondCreate(f *fakeServer) func(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	var creates atomic.Int32
	return func(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
		if strings.HasSuffix(r.URL.Path, "/Create") && creates.Add(1) == 2 {
			f.forgetTokens()
		}
		next(w, r)
	}
}

func pass(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) { next(w, r) }

func TestARefusedTokenOnAWriteIsNotResentAndTheNextRunLogsInFresh(t *testing.T) {
	for _, tc := range []struct {
		name    string
		arm     func(*fakeServer)
		serve   func(*fakeServer) func(w http.ResponseWriter, r *http.Request, next http.HandlerFunc)
		creates int
		status  string
	}{
		{name: "a 401 before the write ran", arm: (*fakeServer).expireTokens},
		{name: "a 401 after the write ran", serve: func(f *fakeServer) func(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
			return firstUnauth(f, "Create")
		},
			creates: 1, status: runner.StatusError},
		{name: "an in-band unauthenticated", arm: (*fakeServer).forgetTokensInBand, creates: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeServer()
			t.Cleanup(f.Close)
			var armed atomic.Bool
			serve := pass
			if tc.serve != nil {
				serve = tc.serve(f)
			}
			r := buildRunner(t, testConfig(tokenServer(t, f, false, &armed, serve)), catalogtest.New())
			if rec := run(t, r, testChain(), runner.Options{}); !rec.Passed() {
				t.Fatalf("warm-up: %s", rec.Failure)
			}
			if tc.arm != nil {
				tc.arm(f)
			}
			armed.Store(true)
			creates := f.creates0()
			rec := run(t, r, testChain(), runner.Options{})
			st := rec.Steps[0]
			if rec.Passed() || st.AuthRetry != runner.AuthRetryNotResent || f.creates0()-creates != tc.creates {
				t.Fatalf("a write answered unauthenticated may have been performed, so it is not re-sent: %s retry=%q creates +%d", rec.Status, st.AuthRetry, f.creates0()-creates)
			}
			if tc.status != "" && (st.Status != tc.status || rec.Status != tc.status || !strings.Contains(st.Warning, "not re-sent")) {
				t.Fatalf("a 401 is not a verdict about the rpc: %s/%s %q", st.Status, rec.Status, st.Warning)
			}
			armed.Store(false)
			if rec := run(t, r, testChain(), runner.Options{}); !rec.Passed() || f.loginCount() != 2 {
				t.Fatalf("the rejected token was dropped, so the next run logs in fresh and passes: %s, %d logins", rec.Failure, f.loginCount())
			}
		})
	}
}

func TestARead401IsResentAndTheStepSaysSo(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	var armed atomic.Bool
	armed.Store(true)
	rec := run(t, buildRunner(t, testConfig(tokenServer(t, f, false, &armed, firstUnauth(f, "Fetch"))), catalogtest.New()), testChain(), runner.Options{})
	if st := rec.Steps[1]; !rec.Passed() || st.AuthRetry != runner.AuthRetryResent || !strings.Contains(st.Warning, "re-sent") {
		t.Fatalf("a read answered 401 is re-sent after a fresh login and says so: %s retry=%q %q", rec.Failure, st.AuthRetry, st.Warning)
	}
}

func TestInBandUnauthenticatedIsReadOnlyAtTheConfiguredPath(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	srv.inBandPath = "status.code"
	r := newRunner(t, srv)
	run(t, r, testChain(), runner.Options{})
	srv.forgetTokensInBand()
	run(t, r, testChain(), runner.Options{})
	if srv.loginCount() != 1 {
		t.Fatalf("the verdict is spelt at status.code, which the config does not declare, so nothing reads as unauthenticated; logins=%d", srv.loginCount())
	}
}

func TestAFreshTokenTheBackendRefusesIsCalledAPossibleAuthRegression(t *testing.T) {
	for _, tc := range []struct{ rpc, step, evidence string }{
		{"Create", "create", "a login in this run had just issued"},
		{"Fetch", "fetch", "re-sent with the new token"},
	} {
		f := newFakeServer()
		var armed atomic.Bool
		armed.Store(true)
		rec := run(t, buildRunner(t, testConfig(tokenServer(t, f, false, &armed, alwaysUnauth(tc.rpc))), catalogtest.New()), testChain(), runner.Options{KeepGoing: true})
		f.Close()
		st := stepByID(t, rec, tc.step)
		if st.Status != runner.StatusError || !strings.Contains(st.Error, "auth regression") || !strings.Contains(st.Error, tc.evidence) || !runner.RefusedFreshToken(st) || strings.Contains(st.Error, "check the credentials") {
			t.Fatalf("%s: the step says it may be an auth regression, with the evidence %q: %s %q", tc.rpc, tc.evidence, st.Status, st.Error)
		}
	}
}

func TestATokenAcceptedEarlierThenRefusedIsARestartUnlessItDiedBeforeItsStatedExpiry(t *testing.T) {
	c := normalized(t, flow(step("first", "ThingService/Create", thing("a"), okExpect()...), step("second", "ThingService/Create", thing("b"), okExpect()...)))
	for _, expiring := range []bool{false, true} {
		f := newFakeServer()
		var armed atomic.Bool
		armed.Store(true)
		cfg := testConfig(tokenServer(t, f, expiring, &armed, forgetOnSecondCreate(f)))
		cfg.Root = t.TempDir()
		st := run(t, buildRunner(t, cfg, catalogtest.New()), c, runner.Options{KeepGoing: true}).Steps[1]
		f.Close()
		if st.Status != runner.StatusError || strings.Contains(st.Error, "auth regression") || runner.RefusedFreshToken(st) {
			t.Fatalf("expiring=%v: a token accepted earlier in the run is not an auth regression: %s %q", expiring, st.Status, st.Error)
		}
		if !expiring {
			if !strings.Contains(st.Error, "restarted mid-run") {
				t.Fatalf("the step says the backend likely restarted mid-run: %q", st.Error)
			}
			continue
		}
		if len(st.TokenRefused) != 1 {
			t.Fatalf("the step records the refused token's issue and expiry: %+v", st.TokenRefused)
		}
		r := st.TokenRefused[0]
		if stated, ok := r.Stated(); !ok || stated < 59*time.Minute || stated > 61*time.Minute || !r.Early() || r.Cached || r.FirstUse || r.Token == "" || strings.Contains(r.Token, "token-") {
			t.Fatalf("a token minted in this run, accepted, then refused an hour early, recorded by fingerprint: %+v", r)
		}
		if strings.Contains(st.Error, "likely restarted") || !strings.Contains(st.Error, "after issue although the login said it expires in 3600s") {
			t.Fatalf("nothing shows a restart; the step says how long after issue it was refused: %q", st.Error)
		}
	}
}

func TestACachedTokenTheBackendRefuses(t *testing.T) {
	inBand := func(f *fakeServer) func(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
		return func(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
			if !strings.HasSuffix(r.URL.Path, "/Create") {
				next(w, r)
				return
			}
			f.handle(&bufferWriter{h: http.Header{}}, r)
			writeJSON(w, 200, map[string]any{"error": map[string]any{"code": "unauthenticated", "message": "session expired"}})
		}
	}
	twoCreates := normalized(t, flow(step("first", "ThingService/Create", thing("a"), okExpect()...), step("second", "ThingService/Create", thing("b"), okExpect()...)))
	for _, tc := range []struct {
		name    string
		arm     func(*fakeServer)
		serve   func(*fakeServer) func(w http.ResponseWriter, r *http.Request, next http.HandlerFunc)
		c       *chain.Chain
		at      int
		passed  bool
		retry   string
		warns   string
		creates int32
		check   func(*testing.T, *runner.StepRecord)
	}{
		{name: "the restarted backend refuses it, so it is replaced and the write resent", arm: (*fakeServer).forgetTokens, passed: true, retry: runner.AuthRetryResent, warns: "cache", creates: 1,
			check: func(t *testing.T, st *runner.StepRecord) {
				if len(st.TokenRefused) != 1 || !st.TokenRefused[0].Cached || !st.TokenRefused[0].FirstUse || !st.TokenRefused[0].Early() {
					t.Fatalf("the refusal of an untried cached token is recorded as such: %+v", st.TokenRefused)
				}
				if _, ok := st.TokenRefused[0].Age(); !ok || !strings.Contains(st.Warning, "after issue although the login said it expires in 3600s") {
					t.Fatalf("the warning says the token died before its stated expiry: %q", st.Warning)
				}
			}},
		{name: "accepted then refused reads as a restart", serve: forgetOnSecondCreate, c: twoCreates, at: 1, check: func(t *testing.T, st *runner.StepRecord) {
			if st.Status != runner.StatusError || strings.Contains(st.Error, "check the credentials") || !strings.Contains(st.Error, "restarted") {
				t.Fatalf("the backend accepted this token earlier in the run, so the credentials are not the cause: %s %q", st.Status, st.Error)
			}
		}},
		{name: "refused and then a fresh one too", serve: func(*fakeServer) func(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
			return alwaysUnauth("Create")
		},
			retry: runner.AuthRetryResent, check: func(t *testing.T, st *runner.StepRecord) {
				if !runner.RefusedFreshToken(st) || strings.Contains(st.Warning, "a restart or a revoke") {
					t.Fatalf("the freshly issued token was refused too, so it is not about the cached token: %q", st.Warning)
				}
			}},
		{name: "refused in-band on a write is not sent twice", serve: inBand, retry: runner.AuthRetryNotResent, warns: "not re-sent", creates: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeServer()
			t.Cleanup(f.Close)
			var armed atomic.Bool
			serve := pass
			if tc.serve != nil {
				serve = tc.serve(f)
			}
			cfg := testConfig(tokenServer(t, f, true, &armed, serve))
			cfg.Root = t.TempDir()
			if rec := run(t, buildRunner(t, cfg, catalogtest.New()), testChain(), runner.Options{}); !rec.Passed() {
				t.Fatalf("setup: the first run must pass: %s", rec.Failure)
			}
			if tc.arm != nil {
				tc.arm(f)
			}
			armed.Store(true)
			creates := f.creates0()
			c := tc.c
			if c == nil {
				c = testChain()
			}
			rec := run(t, buildRunner(t, cfg, catalogtest.New()), c, runner.Options{KeepGoing: true})
			st := rec.Steps[tc.at]
			if rec.Passed() != tc.passed || (tc.retry != "" && st.AuthRetry != tc.retry) || !strings.Contains(st.Warning, tc.warns) || (tc.creates != 0 && int32(f.creates0()-creates) != tc.creates) {
				t.Fatalf("passed=%v retry=%q warning=%q creates +%d", rec.Passed(), st.AuthRetry, st.Warning, f.creates0()-creates)
			}
			if tc.check != nil {
				tc.check(t, st)
			}
		})
	}
}
