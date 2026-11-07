package database

import "time"

// ---------------------------------------------------------------------------
// T8.1 — Webhook Endpoints & Deliveries
// ---------------------------------------------------------------------------

// WebhookEndpoint is an outbound webhook subscription for a user.
type WebhookEndpoint struct {
	ID         int64       `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID     string      `gorm:"type:text;not null" json:"user_id"`
	URL        string      `gorm:"type:text;not null" json:"url"`
	Events     StringSlice `gorm:"type:jsonb;not null;default:'[]'" json:"events"`
	SecretHash string      `gorm:"type:text;not null;default:''" json:"-"`
	Active     bool        `gorm:"default:true" json:"active"`
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
}

func (WebhookEndpoint) TableName() string { return "webhook_endpoints" }

// WebhookDelivery records a single delivery attempt for a webhook.
type WebhookDelivery struct {
	ID           int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	EndpointID   int64     `gorm:"not null" json:"endpoint_id"`
	EventID      string    `gorm:"type:text;not null;default:''" json:"event_id"`
	EventType    string    `gorm:"type:text;not null;default:''" json:"event_type"`
	Status       string    `gorm:"type:text;not null;default:'pending'" json:"status"`
	Attempts     int       `gorm:"default:0" json:"attempts"`
	ResponseCode *int      `json:"response_code,omitempty"`
	ResponseBody string    `gorm:"type:text" json:"response_body,omitempty"`
	DurationMs   *int      `json:"duration_ms,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

func (WebhookDelivery) TableName() string { return "webhook_deliveries" }

// ---------------------------------------------------------------------------
// T8.3 — Integration Tokens
// ---------------------------------------------------------------------------

// IntegrationToken is an inbound receiver authentication token.
//
// The credential is never stored in plaintext. Token holds the AES-256-GCM
// ciphertext (enc:v1: envelope, row-bound AAD — see
// internal/handlers/integrations/token_secret.go) and TokenHash holds the
// deterministic HMAC blind index the receiver matches on, because a random-nonce
// ciphertext cannot be found with an equality query. Neither field is ever
// serialised to a client.
type IntegrationToken struct {
	ID        int64       `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID    string      `gorm:"type:text;not null" json:"user_id"`
	Token     string      `gorm:"type:text;not null;default:''" json:"-"`
	TokenHash string      `gorm:"type:text;not null;default:''" json:"-"`
	Name      string      `gorm:"type:text;not null;default:''" json:"name"`
	Scopes    StringSlice `gorm:"type:jsonb;not null;default:'[]'" json:"scopes"`
	Active    bool        `gorm:"default:true" json:"active"`
	CreatedAt time.Time   `json:"created_at"`
}

func (IntegrationToken) TableName() string { return "integration_tokens" }

// WebhookModels returns all webhook-related models for AutoMigrate.
func WebhookModels() []interface{} {
	return []interface{}{
		&WebhookEndpoint{},
		&WebhookDelivery{},
		&IntegrationToken{},
	}
}
