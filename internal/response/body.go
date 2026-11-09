package response

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/gofiber/fiber/v2"
)

// ParseBody decodes a JSON request body into dst, writes the 400 envelope and
// returns ErrHandled when it cannot, and is the single way every handler reads a
// JSON body (issue #88).
//
// It exists because `c.BodyParser` reports one error for two different failures:
// a body that is not JSON at all, and a body that is valid JSON but whose fields
// do not match the struct. Both used to be answered with the literal
// "invalid JSON body", which sent the reader hunting for a syntax error that did
// not exist. Here the syntax case keeps that message, and the shape case names
// the offending field and what was expected — e.g.
// `ids.tmdb: expected a number or a numeric string`.
//
// Callers use it as:
//
//	if err := response.ParseBody(c, &req); err != nil {
//		return err
//	}
func ParseBody(c *fiber.Ctx, dst any) error {
	body := c.Body()
	if len(body) == 0 {
		_ = BadRequest(c, "invalid JSON body")
		return ErrHandled
	}
	if err := json.Unmarshal(body, dst); err != nil {
		_ = BadRequest(c, bodyErrorMessage(err))
		return ErrHandled
	}
	return nil
}

// bodyErrorMessage turns a decode error into the message the client sees: the
// bare "invalid JSON body" for a syntax failure (the message the contract and
// existing clients expect), and a field-naming message for a shape failure.
func bodyErrorMessage(err error) string {
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) {
		return "invalid JSON body"
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return typeErrorMessage(typeErr)
	}
	// A custom UnmarshalJSON (e.g. FlexibleID or the sync ids decoder) already
	// returns a message naming its field; pass it through.
	return err.Error()
}

// typeErrorMessage spells a JSON type mismatch the way a client reads it: the
// field path, then what the schema wanted, then what arrived.
func typeErrorMessage(e *json.UnmarshalTypeError) string {
	field := e.Field
	if field == "" {
		field = "<body>"
	}
	return fmt.Sprintf("%s: expected %s, got %s", field, jsonTypeName(e.Type), jsonValueName(e.Value))
}

// jsonTypeName renders a Go type as the JSON type a client knows.
//
// It switches on reflect.Kind rather than on the type's name, because the name is
// a Go internal the client has no business seeing: a mismatch against
// checkin.checkinIDs or database.JSONMap used to answer "expected
// checkin.checkinIDs, got a string", which tells a caller nothing they can act
// on. Unknown kinds fall back to "a value", never to the Go type.
func jsonTypeName(t reflect.Type) string {
	if t == nil {
		return "a value"
	}
	if t == reflect.TypeOf(FlexibleID(0)) {
		return "a number or a numeric string"
	}
	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "a number"
	case reflect.String:
		return "a string"
	case reflect.Bool:
		return "a boolean"
	case reflect.Struct, reflect.Map:
		return "an object"
	case reflect.Slice, reflect.Array:
		return "an array"
	}
	return "a value"
}

// jsonValueName names the JSON value that failed to decode, with the article the
// noun needs ("an object", "a string") — "a object" was the old output.
func jsonValueName(value string) string {
	switch value {
	case "":
		return "null"
	case "object":
		return "an object"
	case "array":
		return "an array"
	default:
		return "a " + value
	}
}
