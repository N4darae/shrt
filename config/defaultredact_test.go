package config_test

import (
	"testing"

	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/pathmask"
)

func TestTheDefaultRedactionCoversNumericAndOneTimeSecrets(t *testing.T) {
	r := pathmask.NewRedactor(config.DefaultRedact())
	for _, path := range []string{
		"pin",
		"login.pin",
		"login.user_pin",
		"login.pinCode",
		"login.passcode",
		"login.otp",
		"login.sms_otp",
		"login.secret",
		"login.client_secret",
		"login.password",
		"login.access_token",
	} {
		if !r.MasksValue(path, "123456") {
			t.Errorf("%s is a credential and a default config stores it in clear in every run record", path)
		}
	}
	for _, path := range []string{"login.username", "shipping", "login.opinion", "login.pin_required", "login.spinner"} {
		if r.MasksValue(path, "x") {
			t.Errorf("%s is not a secret; masking it costs a reader the reason an assertion failed", path)
		}
	}
	if r.MasksValue("login.pin", true) {
		t.Error("a bool is never a secret, whatever it is called")
	}
}

func TestAnExplicitRedactListReplacesTheDefaults(t *testing.T) {
	cfg, err := config.Load(write(t, "redact: ['**.only_this']\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Redact) != 1 || cfg.Redact[0] != "**.only_this" {
		t.Fatalf("GRAMMAR says an explicit list replaces the defaults; got %v", cfg.Redact)
	}
	cfg, err = config.Load(write(t, "target: {base_url: 'http://127.0.0.1:1'}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Redact) != len(config.DefaultRedact()) {
		t.Fatalf("GRAMMAR says a config without the key gets the defaults; got %v", cfg.Redact)
	}
}
