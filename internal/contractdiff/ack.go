package contractdiff

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Acknowledgement is one entry in breaking-changes.yaml: the written record
// that a breaking change was made on purpose. Every field is required.
type Acknowledgement struct {
	Signature string `yaml:"signature"`
	Date      string `yaml:"date"`
	Reason    string `yaml:"reason"`
}

// Acknowledgements is the parsed acknowledgement file.
type Acknowledgements struct {
	Version      int               `yaml:"version"`
	Acknowledged []Acknowledgement `yaml:"acknowledged"`
}

// LoadAcknowledgements reads and validates an acknowledgement file.
func LoadAcknowledgements(path string) (*Acknowledgements, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	a, err := ParseAcknowledgements(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return a, nil
}

// ParseAcknowledgements parses and validates an acknowledgement file. Unknown
// keys are rejected — a misspelled key would otherwise silently drop the field
// and leave the acknowledgement meaningless.
func ParseAcknowledgements(data []byte) (*Acknowledgements, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var a Acknowledgements
	if err := dec.Decode(&a); err != nil {
		return nil, fmt.Errorf("parse acknowledgements: %w", err)
	}
	if err := a.validate(); err != nil {
		return nil, err
	}
	return &a, nil
}

func (a *Acknowledgements) validate() error {
	var problems []string
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	if a.Version != 1 {
		add("version must be 1, got %d", a.Version)
	}

	seen := map[string]bool{}
	for i, e := range a.Acknowledged {
		where := fmt.Sprintf("acknowledged[%d]", i)
		sig := strings.TrimSpace(e.Signature)
		switch {
		case sig == "":
			add("%s: signature is required", where)
		case seen[sig]:
			add("%s: duplicate signature %q", where, sig)
		default:
			seen[sig] = true
		}
		if strings.TrimSpace(e.Reason) == "" {
			add("%s (%s): reason is required", where, sig)
		} else if strings.Contains(e.Reason, "\n") {
			add("%s (%s): reason must be one line", where, sig)
		}
		if _, err := time.Parse("2006-01-02", strings.TrimSpace(e.Date)); err != nil {
			add("%s (%s): date must be YYYY-MM-DD, got %q", where, sig, e.Date)
		}
	}

	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("invalid acknowledgement file:\n  - %s", strings.Join(problems, "\n  - "))
}

// signatureSet returns the acknowledged signatures as a set.
func (a *Acknowledgements) signatureSet() map[string]bool {
	out := make(map[string]bool, len(a.Acknowledged))
	for _, e := range a.Acknowledged {
		out[strings.TrimSpace(e.Signature)] = true
	}
	return out
}
