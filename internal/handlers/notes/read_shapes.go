package notes

import "github.com/haaku-tv/haaku/internal/database"

// NoteResponse is the canonical note shape, exported as an alias so the
// public-profile read surface (/v1/users/{id}/notes/...) returns the same note
// body /v1/users/me/notes returns, rather than a second definition.
type NoteResponse = noteResponse

// ToNoteResponse renders a note row.
func ToNoteResponse(n *database.UserNote) NoteResponse {
	return toNoteResponse(n)
}
