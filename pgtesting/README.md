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
	pgtesting.CreatePostgresServer()
	code := m.Run()
	pgtesting.DestroyPostgresServer()
	os.Exit(code)
}

func TestXXX(t *testing.T) {
	t.Parallel()

	database := mbstesting.NewPostgresDatabase(t)
	// Use database.ConnString() to get connection string of the database
    ...
}
```
