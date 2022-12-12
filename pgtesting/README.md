# How to use ?

```
package main

import (
	"context"
	"os"
	"testing"

	"github.com/formancehq/go-libs/pgtesting/pkg"
)

func TestMain(m *testing.M) {
	if err := pgtesting.CreatePostgresServer(); err != nil {
	    log.Fatal(err)
	}
	code := m.Run()
	if err := pgtesting.DestroyPostgresServer(); err != nil {
	    log.Fatal(err)
	}
	os.Exit(code)
}

func TestXXX(t *testing.T) {
	t.Parallel()

	database := mbstesting.NewPostgresDatabase(t)
	// Use database.ConnString() to get connection string of the database
    ...
}
```
