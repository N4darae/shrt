package doctor_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/doctor"
)

func TestFindingLevelsAreStringsInJSON(t *testing.T) {
	raw, err := json.Marshal([]doctor.Finding{
		{Check: "a", Level: doctor.LevelOK}, {Check: "b", Level: doctor.LevelWarn}, {Check: "c", Level: doctor.LevelError},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"Level":"ok"`, `"Level":"WARN"`, `"Level":"FAIL"`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("doctor -json must name each level as the text output does (%s):\n%s", want, raw)
		}
	}
	var back []doctor.Finding
	if err := json.Unmarshal(raw, &back); err != nil || back[1].Level != doctor.LevelWarn || back[2].Level != doctor.LevelError {
		t.Fatalf("the JSON must read back into the same levels: %v %+v", err, back)
	}
}
