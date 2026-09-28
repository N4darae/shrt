package runner_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/pathmask"
	"github.com/N4darae/shrt/runner"
)

const scrubPassword = "hunter2-pw-9f3a"

func scrubRunner(t *testing.T, srv *fakeServer, user string) *runner.Runner {
	t.Helper()
	t.Setenv("SHRT_TEST_SCRUB_USER", user)
	t.Setenv("SHRT_TEST_SCRUB_PW", scrubPassword)
	cfg := testConfig(srv.URL)
	cfg.Auth.Body = map[string]any{"username": "${env.SHRT_TEST_SCRUB_USER}", "password": "${env.SHRT_TEST_SCRUB_PW}"}
	return buildRunner(t, cfg, catalogtest.New())
}

func loginStep(id string, body map[string]any, export map[string]string, expect ...chain.Expectation) *chain.Step {
	return &chain.Step{ID: id, Call: "AuthService/Login", SkipAuth: true, Body: body, Export: export, Expect: expect}
}

func TestASecretNeverReachesTheRecordButIsStillSent(t *testing.T) {
	t.Setenv("SHRT_TEST_OTHER_PW", "other-pw-7c1d")
	t.Setenv("SHRT_TEST_OTHER_USER", "otheruser")
	const mask = pathmask.MaskRedacted
	loginAlice := func(password string, extra ...chain.Expectation) *chain.Step {
		return loginStep("login", map[string]any{"username": "alice", "password": password}, nil, extra...)
	}
	withVars := func(redact []string, vars map[string]any, user, password string) *chain.Chain {
		c := flow(loginStep("login", map[string]any{"username": user, "password": password}, nil))
		c.Redact, c.Vars = redact, vars
		return c
	}
	redacted := func(redact []string, steps ...*chain.Step) *chain.Chain {
		c := flow(steps...)
		c.Redact = redact
		return c
	}
	for _, tc := range []struct {
		name    string
		scrub   string
		c       *chain.Chain
		opts    runner.Options
		hidden  []string
		shown   []string
		vars    map[string]any
		exports map[string]any
		request map[int]string
		sent    map[string]any
		check   func(*testing.T, *runner.Record, *fakeServer)
	}{
		{name: "a redacted password and token", c: redacted([]string{"**.password", "**.access_token"},
			loginStep("login", map[string]any{"username": "staff", "password": "hunter2"}, map[string]string{"token": "access_token"})),
			hidden: []string{"hunter2", "token-1"}, request: map[int]string{0: "<redacted>"}, exports: map[string]any{"token": mask}, sent: map[string]any{"password": "hunter2"}},
		{name: "an exported secret still resolves", c: redacted(config.DefaultRedact(),
			loginStep("login", map[string]any{"username": "alice", "password": "hunter2"}, map[string]string{"session": "access_token"}),
			step("create", "ThingService/Create", map[string]any{"name": "${exports.session}", "kind": "KIND_A"})),
			hidden: []string{"token-1"}, exports: map[string]any{"session": mask}, check: func(t *testing.T, rec *runner.Record, f *fakeServer) {
				if f.bodies[len(f.bodies)-1]["name"] != "token-1" || stepByID(t, rec, "login").Exported["session"] != mask {
					t.Fatalf("a later step resolves the real value and the per-step copy is masked too: %v", f.bodies)
				}
			}},
		{name: "a non-secret export and expectation", c: testChain(), opts: runner.Options{Redact: config.DefaultRedact()}, exports: map[string]any{"thing_id": "thing-1"},
			check: func(t *testing.T, rec *runner.Record, f *fakeServer) {
				if expectResult(t, rec, "id").Got == mask {
					t.Fatal("id matches no redact pattern")
				}
			}},
		{name: "an expectation on a secret field", c: redacted(config.DefaultRedact(), loginAlice("hunter2", chain.Expectation{Path: "error.code", Equals: "OK"}, chain.Expectation{Path: "access_token", NotEmpty: true})),
			hidden: []string{"token-1"}, check: func(t *testing.T, rec *runner.Record, f *fakeServer) {
				if expectResult(t, rec, "access_token").Got != mask {
					t.Fatal("the expectation result masks the field the stored response masks")
				}
			}},
		{name: "a redacted field of the record", c: func() *chain.Chain { c := testChain(); c.Redact = []string{"**.name"}; return c }(), check: func(t *testing.T, rec *runner.Record, f *fakeServer) {
			var decoded map[string]any
			_ = json.Unmarshal(stepByID(t, rec, "fetch").Response, &decoded)
			if decoded["name"] != mask {
				t.Fatalf("want name redacted, got %v", decoded["name"])
			}
		}},
		{name: "change-password fields", c: redacted(config.DefaultRedact(), loginStep("change", map[string]any{"username": "alice", "old_password": "hunter2", "new_password": "hunter3"}, nil)),
			hidden: []string{"hunter2", "hunter3"}, sent: map[string]any{"old_password": "hunter2", "new_password": "hunter3"}},
		{name: "a var named password", c: withVars([]string{"**.password"}, map[string]any{"password": "hunter2", "staff_user": "alice"}, "${vars.staff_user}", "${vars.password}"),
			vars: map[string]any{"password": mask, "staff_user": "alice"}, sent: map[string]any{"password": "hunter2"}},
		{name: "a prefixed password var under the default list", c: withVars(config.DefaultRedact(), map[string]any{"staff_password": "hunter2", "staff_user": "alice"}, "${vars.staff_user}", "${vars.staff_password}"),
			vars: map[string]any{"staff_password": mask}},
		{name: "a secret var passed as an option", c: withVars(config.DefaultRedact(), map[string]any{"staff_user": "alice"}, "${vars.staff_user}", "${vars.staff_password}"),
			opts: runner.Options{Vars: map[string]any{"staff_password": "from-the-flag"}}, hidden: []string{"from-the-flag"}},
		{name: "a var read into a redacted field", c: withVars([]string{"**.password"}, map[string]any{"pw": "placeholder", "staff_user": "alice"}, "${vars.staff_user}", "${vars.pw}"),
			opts: runner.Options{Vars: map[string]any{"pw": "hunter2-x9"}}, hidden: []string{"hunter2-x9"}, vars: map[string]any{"staff_user": "alice"}, sent: map[string]any{"password": "hunter2-x9"}},
		{name: "a fixture var inside a secret field", c: withVars([]string{"**.password"}, map[string]any{"tag": "fixture-tag-q7"}, "u-${vars.tag}", "wrong-${vars.tag}"),
			hidden: []string{"wrong-fixture-tag-q7"}, shown: []string{"u-fixture-tag-q7"}, vars: map[string]any{"tag": "fixture-tag-q7"}},
		{name: "a var used only in a secret field", c: withVars([]string{"**.password"}, map[string]any{"tag": "fixture-tag-q7"}, "alice", "wrong-${vars.tag}"),
			hidden: []string{"fixture-tag-q7"}, vars: map[string]any{"tag": mask}},
		{name: "a var that is the whole password", c: withVars([]string{"**.password"}, map[string]any{"pw": "hunter2-x9"}, "u-${vars.pw}", "${vars.pw}"),
			hidden: []string{"hunter2-x9"}, vars: map[string]any{"pw": mask}},
		{name: "a var named like a secret", c: withVars([]string{"**.password"}, map[string]any{"pw": "hunter2-x9"}, "u-${vars.pw}", "x-${vars.pw}"),
			hidden: []string{"hunter2-x9"}, vars: map[string]any{"pw": mask}},
		{name: "a password and a reused token", scrub: "staff", c: redacted([]string{"**.access_token", "**.*password"},
			&chain.Step{ID: "login", Call: "AuthService/Login", Body: map[string]any{"username": "staff", "password": "${env.SHRT_TEST_SCRUB_PW}"}, Export: map[string]string{"tok": "access_token"}, Expect: okExpect()},
			step("echo_token", "ThingService/Fetch", byID("${tok}"), chain.Expectation{Path: "id", Equals: "${tok}"}, chain.Expectation{Path: "id", Contains: "${login.access_token}"}),
			step("echo_password", "ThingService/Fetch", byID("${env.SHRT_TEST_SCRUB_PW} x"), chain.Expectation{Path: "id", Contains: "${env.SHRT_TEST_SCRUB_PW}"})),
			opts: runner.Options{Redact: []string{"**.access_token", "**.*password"}}, hidden: []string{scrubPassword, "token:staff"}, request: map[int]string{2: "<redacted> x"}},
		{name: "a token the middleware fetched", scrub: "staff", c: flow(step("create", "ThingService/Create", thing("widget"), okExpect()...), step("fetch", "ThingService/Fetch", byID("token-1"), okExpect()...)),
			hidden: []string{"token:staff"}},
		{name: "a token an in-chain login returned, read directly", scrub: "staff", c: flow(
			loginStep("login_other", map[string]any{"username": "other", "password": "pw-other"}, nil, okExpect()...),
			step("echo_token", "ThingService/Fetch", byID("${login_other.access_token}"), chain.Expectation{Path: "id", Equals: "${login_other.access_token}"})),
			opts: runner.Options{Redact: config.DefaultRedact()}, hidden: []string{"token:other"}, request: map[int]string{1: "<redacted>"}},
		{name: "a token at an unredacted token path", scrub: "staff", c: flow(
			loginStep("login_other", map[string]any{"username": "other", "password": "pw-other"}, nil, okExpect()...),
			step("echo_token", "ThingService/Fetch", byID("${login_other.access_token}"), okExpect()...)),
			opts: runner.Options{Redact: []string{"**.*password"}}, hidden: []string{"token:other"}},
		{name: "an env password a step body reads", scrub: "staff", c: flow(
			step("echo_first", "ThingService/Fetch", byID("early ${env.SHRT_TEST_OTHER_PW}"), okExpect()...),
			loginStep("login_other", map[string]any{"username": "${env.SHRT_TEST_OTHER_USER}", "password": "${env.SHRT_TEST_OTHER_PW}"}, nil, okExpect()...),
			step("echo_password", "ThingService/Fetch", byID("pw ${env.SHRT_TEST_OTHER_PW} by otheruser"), chain.Expectation{Path: "id", Contains: "${env.SHRT_TEST_OTHER_PW}"})),
			opts: runner.Options{Redact: config.DefaultRedact()}, hidden: []string{"other-pw-7c1d"}, request: map[int]string{0: "early <redacted>", 2: "pw <redacted> by otheruser"}},
		{name: "a login username clerk", scrub: "clerk", c: flow(step("create", "ThingService/Create", thing("clerk made"), okExpect()...),
			step("echo_password", "ThingService/Fetch", byID("${env.SHRT_TEST_SCRUB_PW} x"), okExpect()...)),
			opts: runner.Options{Redact: config.DefaultRedact()}, hidden: []string{scrubPassword}, request: map[int]string{0: "clerk made", 1: "<redacted> x"}},
		{name: "a login username admin", scrub: "admin", c: flow(step("create", "ThingService/Create", thing("admin made"), okExpect()...),
			step("echo_password", "ThingService/Fetch", byID("${env.SHRT_TEST_SCRUB_PW} x"), okExpect()...)),
			opts: runner.Options{Redact: config.DefaultRedact()}, hidden: []string{scrubPassword}, request: map[int]string{0: "admin made", 1: "<redacted> x"}},
		{name: "a token echoed as a response object key", scrub: "staff", c: flow(step("fetch", "ThingService/Fetch", byID("a"), okExpect()...)),
			opts: runner.Options{Redact: config.DefaultRedact()}, hidden: []string{"token:staff"}, check: func(t *testing.T, rec *runner.Record, f *fakeServer) {
				if !strings.Contains(rec.Steps[0].Error, "<redacted>") {
					t.Fatalf("the drift error names the key by the redaction marker, got %s", rec.Steps[0].Error)
				}
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeServer()
			t.Cleanup(f.Close)
			r := newRunner(t, f)
			if tc.scrub != "" {
				r = scrubRunner(t, f, tc.scrub)
				f.tokenKey = tc.name == "a token echoed as a response object key"
			}
			rec := run(t, r, normalized(t, tc.c), tc.opts)
			if rec.Status != runner.StatusPassed && tc.check == nil {
				t.Fatalf("scrubbing is applied after evaluation, so the chain passes: %s %s", rec.Status, rec.Failure)
			}
			text := recordText(t, rec)
			for _, s := range tc.hidden {
				if user, ok := strings.CutPrefix(s, "token:"); ok {
					s = f.tokenIssuedTo(t, user)
				}
				if strings.Contains(text, s) {
					t.Errorf("%q reached the run record: %s", s, text)
				}
			}
			for _, s := range tc.shown {
				if !strings.Contains(text, s) {
					t.Errorf("%q is not a secret and must stay in the record: %s", s, text)
				}
			}
			for k, v := range tc.vars {
				if rec.Vars[k] != v {
					t.Errorf("vars[%s] = %v, want %v", k, rec.Vars[k], v)
				}
			}
			for k, v := range tc.exports {
				if rec.Exports[k] != v {
					t.Errorf("exports[%s] = %v, want %v", k, rec.Exports[k], v)
				}
			}
			for i, want := range tc.request {
				if got := string(rec.Steps[i].Request); !strings.Contains(got, want) {
					t.Errorf("step %d request %s lacks %q", i, got, want)
				}
			}
			for k, v := range tc.sent {
				if f.bodies[0][k] != v {
					t.Errorf("redaction must not change what is sent: %s = %v", k, f.bodies[0][k])
				}
			}
			if tc.check != nil {
				tc.check(t, rec, f)
			}
		})
	}
}

func TestAnEmptySecretIsNotMasked(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	srv.refuseLogin = true
	redact := runner.Options{Redact: []string{"**.access_token", "**.password"}}
	rec := run(t, newRunner(t, srv), normalized(t, loginChain(chain.Expectation{Path: "error.code", Equals: "unauthenticated"}, chain.Expectation{Path: "access_token", Equals: ""})), redact)
	var body, req map[string]any
	_ = json.Unmarshal(rec.Steps[0].Response, &body)
	_ = json.Unmarshal(rec.Steps[0].Request, &req)
	if body["access_token"] != "" || expectResult(t, rec, "access_token").Got == pathmask.MaskRedacted || req["password"] != pathmask.MaskRedacted {
		t.Fatalf("a refused login sent no token and masking \"\" hides exactly that; a non-empty secret is still masked: %v %v", body, req)
	}
	srv.refuseLogin = false
	rec = run(t, newRunner(t, srv), normalized(t, loginChain(chain.Expectation{Path: "error.code", Equals: "OK"})), redact)
	if !strings.Contains(string(rec.Steps[0].Response), `"access_token":"`+pathmask.MaskRedacted+`"`) {
		t.Fatalf("an issued token must still be masked: %s", rec.Steps[0].Response)
	}
}

func echoedHeaderRecord(t *testing.T, header, template string, vars map[string]any) (string, *runner.Record) {
	t.Helper()
	srv := newFakeServer()
	defer srv.Close()
	srv.echoHeader = header
	c := testChain()
	c.Vars = vars
	c.Steps[0].Headers = map[string]string{header: template}
	rec := run(t, newRunner(t, srv), normalized(t, c), runner.Options{})
	return recordText(t, rec), rec
}

func TestAHeaderNamedLikeACredentialIsDigestedAndItsValueScrubbed(t *testing.T) {
	for _, name := range []string{
		"Authorization", "X-Auth-Token", "X-Api-Key", "X-Secret", "X-Access-Token", "Cookie", "X-Hmac", "X-Session-Id", "X-Authtoken",
		"X-Passphrase", "X-Passcode", "X-Pass-Phrase", "X-Pwd", "X-Private-Key", "X-Client-Secret", "X-Credential",
		"X-Apitoken", "X-Apisecret", "X-Csrftoken", "X-Clientsecret", "X-Sessiontoken", "X-Refreshtoken",
		"X-Mfa-Code", "X-Totp", "X-2fa-Code", "X-Oauth", "X-Authz", "X-Passw0rd", "X-Recovery-Code",
		"X-Magic-Link", "X-Signed-Url", "X-Webhooksecret", "X-Xsrf-Header",
	} {
		raw, rec := echoedHeaderRecord(t, name, "${vars.phrase}", map[string]any{"phrase": "correct-horse-battery"})
		if strings.Contains(raw, "correct-horse-battery") || !runner.HeaderDigested(rec.Steps[0].Headers[name]) || rec.Vars["phrase"] == "correct-horse-battery" {
			t.Errorf("%s is named like a credential: recorded as %q, the var and the record must not hold it", name, rec.Steps[0].Headers[name])
		}
	}
	for _, name := range []string{
		"X-Tag", "X-Request-Id", "X-Passenger-Count", "X-Compass", "X-Author", "X-Authority-Region", "X-Secretary", "X-Tokens-Remaining",
		"X-Api-Key-Id", "X-Token-Count", "X-Keyword", "X-Pinned-Version", "X-Secret-Id", "X-Pass-Through", "X-Session-Region", "X-Keyboard", "X-Monkey", "X-Signedness-Mode",
	} {
		raw, rec := echoedHeaderRecord(t, name, "${vars.phrase}", map[string]any{"phrase": "window-seat"})
		if rec.Steps[0].Headers[name] != "window-seat" || strings.Contains(raw, "<redacted>") {
			t.Errorf("%s is not named like a credential, recorded as %q, want it in clear", name, rec.Steps[0].Headers[name])
		}
	}
}

func TestACredentialValueSentInAHeaderIsScrubbedByValue(t *testing.T) {
	t.Setenv("SHRT_TEST_PARTNER_API_KEY", "pk-live-9f8e7d6c5b4a")
	t.Setenv("SHRT_TEST_SHOP_TOKEN", "tok-abcdef123456")
	raw, rec := echoedHeaderRecord(t, "X-Api-Key", "${env.SHRT_TEST_PARTNER_API_KEY}", nil)
	if strings.Contains(raw, "9f8e7d6c5b4a") || !strings.Contains(rec.Failure, "api key <redacted> is not allowed") {
		t.Fatalf("the api key echoed in the error is scrubbed and the rest stays readable: %s", rec.Failure)
	}
	if raw, _ := echoedHeaderRecord(t, "X-Tag", "t-${env.SHRT_TEST_SHOP_TOKEN}", nil); strings.Contains(raw, "abcdef123456") {
		t.Fatalf("an env var named like a credential is a secret wherever it is sent: %s", raw)
	}
	for _, tc := range []struct {
		template string
		step     int
	}{{"${vars.sig}", 0}, {"Sig ${vars.sig}", 0}, {"${vars.sig}", 1}} {
		srv := newFakeServer()
		srv.createRefusal = "denied"
		c := testChain()
		c.Vars = map[string]any{"sig": "sig-value-1234567", "tag": "h1"}
		c.Steps[tc.step].Headers = map[string]string{"X-Passwd": tc.template}
		rec := run(t, newRunner(t, srv), normalized(t, c), runner.Options{})
		srv.Close()
		if strings.Contains(recordText(t, rec), "sig-value-1234567") || rec.Vars["tag"] != "h1" {
			t.Fatalf("%+v: a var sent in a credential header is a secret, even on a step never reached; others stay: %v", tc, rec.Vars)
		}
	}
}

func TestABuildHeaderCarryingAKnownSecretIsScrubbed(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	srv.builds = []string{"v1 dsn=postgres://staff:s3cret-admin@db", "v1 dsn=postgres://staff:s3cret-admin@db", "v2 dsn=postgres://staff:s3cret-admin@db"}
	cfg := testConfig(srv.URL)
	cfg.Target.BuildHeader = "X-Build"
	cfg.Auth.Body = map[string]any{"username": "staff", "password": "s3cret-admin"}
	r, opts, err := runner.NewFromConfig(t.Context(), cfg, catalogtest.New())
	if err != nil {
		t.Fatal(err)
	}
	rec := run(t, r, testChain(), opts)
	if strings.Contains(recordText(t, rec), "s3cret-admin") || !strings.Contains(rec.Build, "v1 dsn=postgres://staff:<redacted>@db") {
		t.Fatalf("record build = %q: the build is kept with only the secret scrubbed", rec.Build)
	}
}
