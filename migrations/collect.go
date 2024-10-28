package migrations

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"

	"github.com/uptrace/bun"
)

//go:generate mockgen -source collect.go -destination collect_generated.go -package migrations . MigrationFileSystem
type MigrationFileSystem interface {
	ReadDir(dir string) ([]fs.DirEntry, error)
	ReadFile(filename string) ([]byte, error)
}

type notes struct {
	Name string `yaml:"name"`
}

func CollectMigrations(_fs MigrationFileSystem, dir string) ([]Migration, error) {
	return WalkMigrations(_fs, func(entry fs.DirEntry) (*Migration, error) {
		rawNotes, err := _fs.ReadFile(filepath.Join("migrations", entry.Name(), "notes.yaml"))
		if err != nil {
			return nil, fmt.Errorf("failed to read notes.yaml: %w", err)
		}

		notes := &notes{}
		if err := yaml.Unmarshal(rawNotes, notes); err != nil {
			return nil, fmt.Errorf("failed to unmarshal notes.yaml: %w", err)
		}

		sqlFile, err := TemplateSQLFile(_fs, dir, entry.Name(), "up.sql")
		if err != nil {
			return nil, fmt.Errorf("failed to template sql file: %w", err)
		}

		return &Migration{
			Name: notes.Name,
			Up: func(ctx context.Context, db bun.IDB) error {
				_, err := db.ExecContext(ctx, sqlFile)
				return err
			},
		}, nil
	})
}

func WalkMigrations[T any](_fs MigrationFileSystem, transformer func(entry fs.DirEntry) (*T, error)) ([]T, error) {
	entries, err := _fs.ReadDir("migrations")
	if err != nil {
		return nil, err
	}

	slices.SortFunc(entries, func(a, b fs.DirEntry) int {
		fileAVersionAsString := strings.SplitN(a.Name(), "-", 2)[0]
		fileAVersion, err := strconv.ParseInt(fileAVersionAsString, 10, 64)
		if err != nil {
			panic(err)
		}

		fileBVersionAsString := strings.SplitN(b.Name(), "-", 2)[0]
		fileBVersion, err := strconv.ParseInt(fileBVersionAsString, 10, 64)
		if err != nil {
			panic(err)
		}

		return int(fileAVersion - fileBVersion)
	})

	ret := make([]T, len(entries))
	for i, entry := range entries {
		transformed, err := transformer(entry)
		if err != nil {
			return nil, fmt.Errorf("failed to transform entry: %w", err)
		}
		ret[i] = *transformed
	}

	return ret, nil
}

func TemplateSQLFile(_fs MigrationFileSystem, schema, migrationDir, file string) (string, error) {
	rawSQL, err := _fs.ReadFile(filepath.Join("migrations", migrationDir, file))
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", file, err)
	}

	buf := bytes.NewBuffer(nil)
	err = template.Must(template.New("migration").
		Parse(string(rawSQL))).
		Execute(buf, map[string]any{
			"Schema": schema,
		})
	if err != nil {
		panic(err)
	}

	return buf.String(), nil
}
