package chain

import (
	"fmt"
	"strings"
)

const APIVersion = "shrt/v1"

type Chain struct {
	APIVersion  string         `yaml:"apiVersion" json:"apiVersion"`
	Name        string         `yaml:"name" json:"name"`
	Description string         `yaml:"description,omitempty" json:"description,omitempty"`
	Vars        map[string]any `yaml:"vars,omitempty" json:"vars,omitempty"`
	Volatile    []string       `yaml:"volatile,omitempty" json:"volatile,omitempty"`
	Redact      []string       `yaml:"redact,omitempty" json:"redact,omitempty"`
	Steps       []*Step        `yaml:"steps" json:"steps"`

	SourcePath string `yaml:"-" json:"-"`
}

type Step struct {
	ID          string            `yaml:"id" json:"id"`
	Description string            `yaml:"description,omitempty" json:"description,omitempty"`
	Call        string            `yaml:"call" json:"call"`
	Body        map[string]any    `yaml:"body,omitempty" json:"body,omitempty"`
	Headers     map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	Expect      []Expectation     `yaml:"expect,omitempty" json:"expect,omitempty"`
	Export      map[string]string `yaml:"export,omitempty" json:"export,omitempty"`
	Auth        string            `yaml:"auth,omitempty" json:"auth,omitempty"`
	SkipAuth    bool              `yaml:"skip_auth,omitempty" json:"skip_auth,omitempty"`
	AllowFail   bool              `yaml:"allow_fail,omitempty" json:"allow_fail,omitempty"`
	Volatile    []string          `yaml:"volatile,omitempty" json:"volatile,omitempty"`
}

func (c *Chain) Step(id string) (*Step, bool) {
	for _, s := range c.Steps {
		if s.ID == id {
			return s, true
		}
	}
	return nil, false
}

func (c *Chain) Normalize() error {
	if c.APIVersion == "" {
		c.APIVersion = APIVersion
	}
	if c.APIVersion != APIVersion {
		return fmt.Errorf("unsupported apiVersion %q, want %q", c.APIVersion, APIVersion)
	}
	if strings.TrimSpace(c.Name) == "" {
		return fmt.Errorf("chain name is required")
	}
	if len(c.Steps) == 0 {
		return fmt.Errorf("chain %q has no steps", c.Name)
	}
	seen := map[string]bool{}
	for i, s := range c.Steps {
		if strings.TrimSpace(s.Call) == "" {
			return fmt.Errorf("step %d: call is required", i+1)
		}
		if s.ID == "" {
			s.ID = defaultStepID(s.Call, i)
		}
		if seen[s.ID] {
			return fmt.Errorf("duplicate step id %q", s.ID)
		}
		seen[s.ID] = true
	}
	return nil
}

func defaultStepID(call string, i int) string {
	name := call
	if idx := strings.LastIndex(call, "/"); idx >= 0 {
		name = call[idx+1:]
	}
	return fmt.Sprintf("%s_%d", toSnake(name), i+1)
}

func toSnake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(r + 32)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
