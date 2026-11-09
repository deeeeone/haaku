package database

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
)

// TextArray is a []string stored in a PostgreSQL TEXT[] column.
//
// database.StringArray is JSONB-backed: its Value marshals a JSON array, which
// is what every jsonb-typed StringArray column in this schema expects. It is the
// wrong encoding for a TEXT[] column — PostgreSQL rejects the JSON literal with
// `malformed array literal: "[\"profile\"]"` (SQLSTATE 22P02). migrations/sql
// creates oauth_grants.scopes and oauth_tokens.scopes as TEXT[], so those models
// use TextArray instead of StringArray.
//
// Scan accepts both encodings (JSON first, then the `{elem,elem}` array
// literal), so a column later moved to jsonb still decodes. Only the write side
// is specific to TEXT[].
type TextArray []string

// Scan implements sql.Scanner.
func (a *TextArray) Scan(value interface{}) error {
	if value == nil {
		*a = nil
		return nil
	}
	var raw []byte
	switch v := value.(type) {
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	default:
		return fmt.Errorf("TextArray.Scan: unsupported type %T", value)
	}
	if len(raw) == 0 || string(raw) == "null" {
		*a = nil
		return nil
	}
	var jsonArr []string
	if err := json.Unmarshal(raw, &jsonArr); err == nil {
		*a = jsonArr
		return nil
	}
	var arr StringArray
	if err := parsePostgresArray(string(raw), &arr); err != nil {
		return err
	}
	*a = TextArray(arr)
	return nil
}

// Value implements driver.Valuer, emitting a PostgreSQL array literal.
func (a TextArray) Value() (driver.Value, error) {
	if a == nil {
		return nil, nil
	}
	var b strings.Builder
	b.WriteByte('{')
	for i, s := range a {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('"')
		// Inside a text[] literal, backslash and double quote are escaped with a
		// backslash.
		s = strings.ReplaceAll(s, `\`, `\\`)
		s = strings.ReplaceAll(s, `"`, `\"`)
		b.WriteString(s)
		b.WriteByte('"')
	}
	b.WriteByte('}')
	return b.String(), nil
}
