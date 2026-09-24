package main

import "testing"

func TestApproverEmailNeedsADottedDomain(t *testing.T) {
	for _, bad := range []string{"a@b", "alice@localhost", "a@b.", "a@.b", "a@b..c"} {
		if _, err := approverEmail(bad); err == nil {
			t.Errorf("-by %q was accepted as an email", bad)
		}
	}
	for _, good := range []string{"lab-tester@example.test", "alice@example.com", "first.last@mail.example.co"} {
		if _, err := approverEmail(good); err != nil {
			t.Errorf("-by %q was refused: %v", good, err)
		}
	}
}
