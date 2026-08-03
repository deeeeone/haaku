package database

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// StringArray is a JSONB-backed string array for GORM -> PostgreSQL.
type StringArray []string

func (a *StringArray) Scan(value interface{}) error {
	if value == nil {
		*a = nil
		return nil
	}
	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return fmt.Errorf("StringArray.Scan: unsupported type %T", value)
	}
	if len(bytes) == 0 || string(bytes) == "null" {
		*a = nil
		return nil
	}
	// Try JSON array first, then PostgreSQL array format
	var strArr []string
	if err := json.Unmarshal(bytes, &strArr); err == nil {
		*a = strArr
		return nil
	}
	// Fallback: pgx text[] format {elem1,elem2}
	return parsePostgresArray(string(bytes), a)
}

func (a StringArray) Value() (driver.Value, error) {
	if a == nil {
		return nil, nil
	}
	return json.Marshal(a)
}

// parsePostgresArray parses PostgreSQL array format {elem1,elem2}
func parsePostgresArray(s string, out *StringArray) error {
	// Simple parser for {elem1,elem2} format
	if len(s) < 2 || s[0] != '{' || s[len(s)-1] != '}' {
		return fmt.Errorf("StringArray: invalid postgres array format: %s", s)
	}
	inner := s[1 : len(s)-1]
	if inner == "" {
		*out = []string{}
		return nil
	}
	// Split by comma, handling quoted strings
	var result []string
	var current []byte
	inQuote := false
	for i := 0; i < len(inner); i++ {
		ch := inner[i]
		if ch == '"' {
			inQuote = !inQuote
			continue
		}
		if ch == ',' && !inQuote {
			result = append(result, string(current))
			current = nil
			continue
		}
		current = append(current, ch)
	}
	if len(current) > 0 {
		result = append(result, string(current))
	}
	*out = result
	return nil
}

// OAuthClient represents a registered OAuth2 application.
type OAuthClient struct {
	ID           string      `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	Name         string      `gorm:"type:text;not null" json:"name"`
	RedirectURIs StringArray `gorm:"type:jsonb;not null;default:'[]'" json:"redirect_uris"`
	SecretHash   string      `gorm:"type:text;not null" json:"-"`
	UserID       string      `gorm:"type:text;not null;index" json:"user_id"`
	CreatedAt    time.Time   `json:"created_at"`
}

func (OAuthClient) TableName() string { return "oauth_clients" }

// OAuthGrant represents an authorization code grant (PKCE-capable).
type OAuthGrant struct {
	ID                  string      `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	ClientID            string      `gorm:"type:uuid;not null" json:"client_id"`
	UserID              string      `gorm:"type:text;not null" json:"user_id"`
	CodeHash            string      `gorm:"type:text;not null;index" json:"-"`
	CodeChallenge       string      `gorm:"type:text" json:"-"`
	CodeChallengeMethod string      `gorm:"type:text;default:S256" json:"code_challenge_method,omitempty"`
	RedirectURI         string      `gorm:"type:text;not null" json:"redirect_uri"`
	Scopes              StringArray `gorm:"type:jsonb;not null;default:'[]'" json:"scopes"`
	ExpiresAt           time.Time   `json:"expires_at"`
	Used                bool        `gorm:"not null;default:false" json:"used"`
	CreatedAt           time.Time   `json:"created_at"`
}

func (OAuthGrant) TableName() string { return "oauth_grants" }

// OAuthToken represents an issued access/refresh token pair.
type OAuthToken struct {
	ID                 string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	GrantID            *string        `gorm:"type:uuid" json:"grant_id,omitempty"`
	ClientID           string         `gorm:"type:uuid;not null" json:"client_id"`
	UserID             string         `gorm:"type:text;not null;index" json:"user_id"`
	TokenHash          string         `gorm:"type:text;not null;index" json:"-"`
	RefreshTokenHash   string         `gorm:"type:text;index" json:"-"`
	RefreshTokenFamily string         `gorm:"type:text" json:"-"`
	Scopes             StringArray    `gorm:"type:jsonb;not null;default:'[]'" json:"scopes"`
	ExpiresAt          time.Time      `json:"expires_at"`
	RefreshExpiresAt   *time.Time     `json:"refresh_expires_at,omitempty"`
	Revoked            bool           `gorm:"not null;default:false" json:"revoked"`
	CreatedAt          time.Time      `json:"created_at"`
	DeletedAt          gorm.DeletedAt `gorm:"index" json:"-"`
}

func (OAuthToken) TableName() string { return "oauth_tokens" }
