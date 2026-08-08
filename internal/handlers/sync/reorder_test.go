package sync

import (
	"errors"
	"testing"
)

func TestValidateReorder(t *testing.T) {
	owned := []int64{1, 2, 3}

	valid := [][]int64{
		{1, 2, 3},
		{3, 2, 1},
		{2, 1, 3},
	}
	for _, ids := range valid {
		if err := validateReorder(ids, owned); err != nil {
			t.Errorf("validateReorder(%v) unexpected error: %v", ids, err)
		}
	}

	invalid := [][]int64{
		{},           // empty (missing items)
		{1, 2},       // partial
		{1, 2, 3, 4}, // extra
		{1, 2, 4},    // foreign id
		{1, 2, 2},    // duplicate
		{1, 2, 0},    // foreign/zero id
	}
	for _, ids := range invalid {
		if err := validateReorder(ids, owned); !errors.Is(err, errIncompleteReorder) {
			t.Errorf("validateReorder(%v) = %v, want errIncompleteReorder", ids, err)
		}
	}

	// Empty watchlist: an empty payload is the complete set.
	if err := validateReorder(nil, nil); err != nil {
		t.Errorf("validateReorder(nil, nil) = %v, want nil", err)
	}
}
