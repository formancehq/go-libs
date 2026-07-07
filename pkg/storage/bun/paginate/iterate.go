package paginate

import (
	"context"
	"fmt"
	"reflect"
)

func Iterate[T any, Q any](ctx context.Context, q Q, iterator func(ctx context.Context, q Q) (*Cursor[T], error), cb func(cursor *Cursor[T]) error) error {

	for {
		cursor, err := iterator(ctx, q)
		if err != nil {
			return err
		}

		if err := cb(cursor); err != nil {
			return err
		}

		if !cursor.HasMore {
			break
		}

		newQuery := reflect.New(reflect.TypeOf(q))
		if err := UnmarshalCursor(cursor.Next, newQuery.Interface()); err != nil {
			return fmt.Errorf("paginating next request: %w", err)
		}

		q = newQuery.Elem().Interface().(Q)
	}

	return nil
}
