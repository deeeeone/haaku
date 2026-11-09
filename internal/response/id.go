package response

import (
	"encoding/json"
	"reflect"
	"strconv"

	"github.com/gofiber/fiber/v2"
)

// FlexibleID decodes an external id that may arrive as a JSON number or as a
// JSON string holding a number, and encodes back as a number. It is the shape
// haaku's own media responses serve (TVDB ids are strings) and Trakt-shaped
// clients send (numbers), so a request that echoes either form decodes instead
// of failing with "invalid JSON body" (issues #87, #88).
//
// Absent and null both decode to 0.
type FlexibleID int

// Int returns the id as a plain int.
func (f FlexibleID) Int() int { return int(f) }

// UnmarshalJSON accepts a number, a numeric string, null, or absent.
func (f *FlexibleID) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		*f = 0
		return nil
	}
	var n int
	if err := json.Unmarshal(data, &n); err == nil {
		*f = FlexibleID(n)
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		if s == "" {
			*f = 0
			return nil
		}
		parsed, perr := strconv.Atoi(s)
		if perr != nil {
			return &json.UnmarshalTypeError{Value: "string", Type: reflect.TypeOf(*f)}
		}
		*f = FlexibleID(parsed)
		return nil
	}
	return &json.UnmarshalTypeError{Value: jsonKind(data), Type: reflect.TypeOf(*f)}
}

// jsonKind names the JSON kind of a raw value from its first byte, so the
// UnmarshalTypeError carries what actually arrived rather than a generic message.
func jsonKind(data []byte) string {
	switch {
	case len(data) == 0:
		return "null"
	case data[0] == '{':
		return "object"
	case data[0] == '[':
		return "array"
	case data[0] == 't' || data[0] == 'f':
		return "bool"
	}
	return "number"
}

// MarshalJSON always writes a JSON number, so a FlexibleID never changes the
// numeric ids the API already serves.
func (f FlexibleID) MarshalJSON() ([]byte, error) { return []byte(strconv.Itoa(int(f))), nil }

// ParseIDParam parses the "id" route param as an int64. On failure it writes a
// 400 envelope and returns ErrHandled so the caller stops immediately instead of
// falling through to a DB lookup with id 0 (which used to answer 404 for input
// the contract advertises as 400 — issue #40).
func ParseIDParam(c *fiber.Ctx, resource string) (int64, error) {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		_ = BadRequest(c, "invalid "+resource+" id")
		return 0, ErrHandled
	}
	return id, nil
}
