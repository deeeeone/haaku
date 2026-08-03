# Webhooks — Trakt-Killer Integration Guide

Trakt-Killer supports outbound webhooks for real-time event delivery and an inbound receiver for arbitrary integrations.

## Outbound Webhooks

Register a webhook endpoint to receive events from your Trakt-Killer account.

### Creating a Webhook Endpoint

```http
POST /v1/webhooks
Authorization: Bearer <your-token>
Content-Type: application/json

{
  "url": "https://my-bot.example.com/webhook",
  "events": ["history.added"]
}
```

Response:
```json
{
  "id": 1,
  "url": "https://my-bot.example.com/webhook",
  "events": ["history.added"],
  "active": true,
  "created_at": "2026-08-02T00:00:00Z",
  "updated_at": "2026-08-02T00:00:00Z",
  "secret": "abc123..."
}
```

> **The secret is only shown once. Store it securely.** The server stores only a hash.

### Webhook Payload Format

Events are delivered as `POST` to your URL with this JSON body:

```json
{
  "event_id": "550e8400-e29b-41d4-a716-446655440000",
  "type": "history.added",
  "created_at": "2026-08-02T12:00:00Z",
  "data": {
    "media_type": "movie",
    "media_id": 12345,
    ...
  },
  "api_version": 1
}
```

### Verifying Signatures

Every delivery includes an `X-TK-Signature` header:

```
X-TK-Signature: t=1754136000,v1=<hex hmac>
```

To verify:

```python
import hmac, hashlib

def verify_signature(secret: str, timestamp: int, body: str, signature: str) -> bool:
    # Hash your stored secret with HMAC-SHA256(secret, "webhook-secret")
    secret_hash = hmac.new(secret.encode(), b"webhook-secret", hashlib.sha256).hexdigest()

    # Compute expected signature
    signed = f"{timestamp}.{body}"
    expected = hmac.new(secret_hash.encode(), signed.encode(), hashlib.sha256).hexdigest()

    return hmac.compare_digest(expected, signature)
```

### Retry Behavior

- Events are retried up to **5 times** with exponential backoff:
  - 1st retry: after 1 minute
  - 2nd retry: after 2 minutes
  - 3rd retry: after 4 minutes
  - 4th retry: after 8 minutes
  - 5th retry: after 16 minutes

- **5 consecutive failures** disables the endpoint automatically.
- Your endpoint should return **2xx** to acknowledge successful receipt.
- If you return **429 Too Many Requests**, the system slows down without counting it as a failure.

### Event Types

| Event | Description |
|---|---|
| `history.added` | Watch history entry added |
| `history.removed` | Watch history entry removed |
| `collection.updated` | Collection updated |
| `watchlist.updated` | Watchlist updated |
| `rating.added` | Rating added |
| `favorite.added` | Favorite added |
| `scrobble` | Scrobble event |
| `checkin` | Check-in event |
| `user.updated` | User profile updated |
| `ping` | Test ping |

### Managing Endpoints

```http
GET    /v1/webhooks            # List your endpoints
POST   /v1/webhooks            # Create endpoint
PUT    /v1/webhooks/:id        # Update endpoint (events, url, active)
DELETE /v1/webhooks/:id        # Delete endpoint
POST   /v1/webhooks/:id/rotate-secret  # Rotate secret
POST   /v1/webhooks/:id/test   # Send test ping
GET    /v1/webhooks/:id/deliveries     # Delivery log
```

### Example: Discord Bot

Here's how a Discord bot would receive `history.added` events:

```python
# bot.py — FastAPI webhook receiver
from fastapi import FastAPI, Request, HTTPException
import hmac, hashlib

app = FastAPI()
WEBHOOK_SECRET = "your-secret-from-create-response"

def verify(body: bytes, timestamp: str, sig: str) -> bool:
    secret_hash = hmac.new(WEBHOOK_SECRET.encode(), b"webhook-secret", hashlib.sha256).hexdigest()
    signed = f"{timestamp}.{body.decode()}"
    expected = hmac.new(secret_hash.encode(), signed.encode(), hashlib.sha256).hexdigest()
    return hmac.compare_digest(expected, sig)

@app.post("/webhook")
async def webhook(req: Request):
    body = await req.body()
    sig_header = req.headers.get("X-TK-Signature", "")

    # Parse t=...,v1=...
    parts = dict(p.split("=", 1) for p in sig_header.split(",") if "=" in p)
    ts = parts.get("t", "")
    v1 = parts.get("v1", "")

    if not verify(body, ts, v1):
        raise HTTPException(401, "Invalid signature")

    event = json.loads(body)

    # Post to Discord channel
    if event["type"] == "history.added":
        media = event["data"]
        await discord_channel.send(
            f"🎬 {media.get('title', 'Unknown')} watched!"
        )

    return {"status": "ok"}
```

## Inbound Integration Receiver

You can also create integration tokens to receive events from external services.

```http
POST /v1/integrations
Authorization: Bearer <your-token>
Content-Type: application/json

{
  "name": "my-automation",
  "scopes": ["*"]
}
```

Response:
```json
{
  "id": 1,
  "name": "my-automation",
  "scopes": ["*"],
  "active": true,
  "created_at": "2026-08-02T00:00:00Z",
  "token": "tk_abcdef..."
}
```

Send events to your token's endpoint:

```http
POST /v1/integrations/tk_abcdef...
Content-Type: application/json

{
  "event": "custom.event",
  "data": { "key": "value" }
}
```

The server acknowledges with **202 Accepted** and processes the event asynchronously.
