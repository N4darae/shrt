package main

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/doctor"
)

func TestDoctorHelpListsEveryCheck(t *testing.T) {
	for _, name := range []string{doctor.CheckBuild, doctor.CheckDocs, doctor.CheckKit, doctor.CheckDescriptor, doctor.CheckIgnored,
		doctor.CheckTokens, doctor.CheckAuth, doctor.CheckConventions, doctor.CheckContracts, doctor.CheckSafeSpots, doctor.CheckUpgrade} {
		if !strings.Contains(doctorHelpTail, "\n  "+name+" ") {
			t.Errorf("doctor -h does not list the %s check:\n%s", name, doctorHelpTail)
		}
	}
}
