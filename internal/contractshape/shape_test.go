package contractshape

import (
	"testing"

	"github.com/haaku-tv/haaku/internal/database"
)

// TestUserEmitsAnIntegerTraktID pins the shape the contract requires of the
// embedded user object: `ids` has exactly two properties, `slug` and `trakt`, and
// `trakt` is a required *integer*.
//
// It used to be null, and the object also carried a `uuid` the contract does not
// declare there. A null in a required integer field makes a conforming client
// reject the whole feed, so this is not a cosmetic difference — and the numeric
// value has to come from somewhere real (see LocalUserNum), not from a constant.
func TestUserEmitsAnIntegerTraktID(t *testing.T) {
	u := &database.User{ID: "local_alice", Username: "alice", Name: "Alice"}
	got := User(u)

	ids, ok := got["ids"].(map[string]any)
	if !ok {
		t.Fatalf("ids is %#v, want an object", got["ids"])
	}
	n, ok := ids["trakt"].(int64)
	if !ok {
		t.Fatalf("ids.trakt = %#v (%T), want an integer", ids["trakt"], ids["trakt"])
	}
	if n <= 0 {
		t.Fatalf("ids.trakt = %d, want a positive number", n)
	}
	if _, has := ids["uuid"]; has {
		t.Fatalf("ids carries uuid %v; the contract's embedded user ids are slug+trakt only", ids["uuid"])
	}
	if ids["slug"] != "alice" {
		t.Fatalf("ids.slug = %#v, want the username", ids["slug"])
	}
}

// TestLocalUserNumIsStableAndDistinct keeps the derivation honest: the same
// account must get the same number in every response, and two accounts must not
// collide on the ids a client keys by.
func TestLocalUserNumIsStableAndDistinct(t *testing.T) {
	if a, b := LocalUserNum("local_alice"), LocalUserNum("local_alice"); a != b {
		t.Fatalf("LocalUserNum is not deterministic: %d != %d", a, b)
	}
	seen := map[int64]string{}
	for _, id := range []string{"local_alice", "local_bob", "user_2abc", "user_2abd", ""} {
		n := LocalUserNum(id)
		if prev, dup := seen[n]; dup {
			t.Fatalf("LocalUserNum(%q) and LocalUserNum(%q) both = %d", id, prev, n)
		}
		seen[n] = id
		if n < 0 {
			t.Fatalf("LocalUserNum(%q) = %d, want a positive id", id, n)
		}
	}
}

// TestUserNilIsMarkedDeleted covers the not-found render: it still has to carry
// the required integer, so a client can parse the row it was told about.
func TestUserNilIsMarkedDeleted(t *testing.T) {
	got := User(nil)
	if got["deleted"] != true {
		t.Fatalf("deleted = %#v, want true", got["deleted"])
	}
	ids := got["ids"].(map[string]any)
	if _, ok := ids["trakt"].(int64); !ok {
		t.Fatalf("ids.trakt = %#v, want an integer even for a deleted user", ids["trakt"])
	}
}
