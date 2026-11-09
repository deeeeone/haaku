// Package shared holds request-side helpers used by more than one handler
// package: the sort resolver and the catalogue-filter applier. They live here
// (rather than in response) because they need the request and, for filters, the
// GORM handle; response stays a leaf package.
package shared

import (
	"fmt"
	"sort"
	"strings"
)

// OrderField is one sortable field: the SQL expression it orders by and
// whether the expression already carries its own ordering.
type OrderField struct {
	// Expr is a SQL expression with no direction suffix.
	Expr string
	// Fixed marks an expression whose ordering cannot take a direction
	// (RANDOM(), a pre-built CASE, ...). Direction is then ignored.
	Fixed bool
}

// SQLOrder resolves a sort name and direction onto an ORDER BY clause.
//
// The contract types sort_by loosely on most operations, so the closed set is
// ours: only a name in fields is accepted, and an unknown name is an error
// naming every accepted value rather than a silent default. That distinction is
// the point of this helper — a client that asked for one ordering and received
// another has been lied to.
//
// how may be empty (defaultHow applies) or asc/desc. Tie-breakers are appended
// with the same direction so a page boundary is stable.
func SQLOrder(sortBy, how string, fields map[string]OrderField, defaultHow string, tieBreakers ...string) (string, error) {
	if how == "" {
		how = defaultHow
	}
	how = strings.ToLower(strings.TrimSpace(how))
	if how != "asc" && how != "desc" {
		return "", fmt.Errorf("sort_how must be one of: asc, desc")
	}
	if sortBy == "" {
		sortBy = firstSort(fields)
	}
	field, ok := fields[sortBy]
	if !ok {
		return "", fmt.Errorf("sort_by must be one of: %s", strings.Join(AcceptedSorts(fields), ", "))
	}
	parts := []string{field.Expr}
	if !field.Fixed {
		parts[0] += " " + how
		for _, tie := range tieBreakers {
			parts = append(parts, tie+" "+how)
		}
	}
	return strings.Join(parts, ", "), nil
}

// NamedOrder resolves a path {sort} segment against a fixed map of
// name -> complete ORDER BY expression. The direction is part of the value
// because these segments name an ordering ("newest"), not a column+direction.
func NamedOrder(sort string, orders map[string]string) (string, error) {
	if expr, ok := orders[sort]; ok {
		return expr, nil
	}
	return "", fmt.Errorf("sort must be one of: %s", strings.Join(sortedKeys(orders), ", "))
}

// AcceptedSorts returns the accepted names in a stable order.
func AcceptedSorts(fields map[string]OrderField) []string {
	out := make([]string, 0, len(fields))
	for name := range fields {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for name := range m {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// firstSort returns the alphabetically first field so an empty sort has a
// deterministic order. Callers that care pass an explicit default.
func firstSort(fields map[string]OrderField) string {
	names := AcceptedSorts(fields)
	if len(names) == 0 {
		return ""
	}
	return names[0]
}
