package comments

import "github.com/haaku-tv/haaku/internal/database"

// CommentResponse is the canonical comment shape. Exported as an alias so the
// public-profile read surface (/v1/users/{id}/comments/... and
// /v1/users/{id}/likes/comments) returns the same comment body the
// /v1/comments/... routes return, rather than a second definition.
type CommentResponse = commentResponse

// ToCommentResponse renders a comment row.
func ToCommentResponse(c *database.Comment) CommentResponse {
	return toCommentResponse(c)
}
