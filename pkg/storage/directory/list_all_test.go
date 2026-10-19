package directory

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/formancehq/go-libs/v5/pkg/storage/object"
)

func TestWholeListingPreservesSingleTraversalBeyondRemotePageSize(t *testing.T) {
	root := t.TempDir()
	for i := range 1001 {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("item-%04d", i)), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	bucket, err := NewBucket(root, object.Limits{MaxObjects: 1002, MaxPages: 1})
	if err != nil {
		t.Fatal(err)
	}
	// A remote-style 1000-entry page loop would exhaust MaxPages; the existing
	// local contract admits this inventory with one bounded traversal.
	got, err := object.ListAll(t.Context(), bucket, "item-", object.Limits{})
	if err != nil || len(got) != 1001 {
		t.Fatalf("single traversal returned %d objects: %v", len(got), err)
	}
	got, err = object.ListAll(t.Context(), bucket, "item-", object.Limits{MaxObjects: 1000})
	if !errors.Is(err, object.ErrListingLimit) || got != nil {
		t.Fatal("whole listing raised caller limit or returned partial inventory")
	}
	got, err = bucket.ListAll(t.Context(), "item-", object.Limits{MaxObjects: 1000})
	if !errors.Is(err, object.ErrListingLimit) || got != nil {
		t.Fatal("direct whole listing raised caller limit or returned partial inventory")
	}
}
