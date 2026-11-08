package paginate

import (
	"context"
	"fmt"
	"math"

	"github.com/uptrace/bun"
)

func usingOffset[Q any, T any](ctx context.Context, sb *bun.SelectQuery, query OffsetPaginatedQuery[Q], builders ...func(query *bun.SelectQuery) *bun.SelectQuery) (*Cursor[T], error) {
	if err := validatePageSize(query.PageSize); err != nil {
		return nil, err
	}
	if query.Offset > math.MaxInt64 {
		return nil, fmt.Errorf("pagination offset exceeds int64 range")
	}

	ret := make([]T, 0)

	sb.Model(&ret)
	for _, builder := range builders {
		sb = sb.Apply(builder)
	}

	if query.Offset > 0 {
		sb = sb.Offset(int64(query.Offset))
	}

	if query.PageSize > 0 {
		sb = sb.Limit(int64(query.PageSize) + 1)
	}

	if err := sb.Scan(ctx); err != nil {
		return nil, err
	}

	var previous, next *OffsetPaginatedQuery[Q]

	// Page with transactions before
	if query.Offset > 0 {
		cp := query
		cp.Offset = 0
		if query.Offset > query.PageSize {
			cp.Offset = query.Offset - query.PageSize
		}
		previous = &cp
	}

	// Page with transactions after
	if query.PageSize != 0 && len(ret) > int(query.PageSize) {
		if query.PageSize > math.MaxInt64-query.Offset {
			return nil, fmt.Errorf("next pagination offset exceeds int64 range")
		}
		cp := query
		cp.Offset = query.Offset + query.PageSize
		next = &cp
		ret = ret[:len(ret)-1]
	}

	return &Cursor[T]{
		PageSize: int(query.PageSize),
		HasMore:  next != nil,
		Previous: previous.EncodeAsCursor(),
		Next:     next.EncodeAsCursor(),
		Data:     ret,
	}, nil
}

func UsingOffset[Q any, T any](ctx context.Context, sb *bun.SelectQuery, query OffsetPaginatedQuery[Q], builders ...func(query *bun.SelectQuery) *bun.SelectQuery) (*Cursor[T], error) {
	return usingOffset[Q, T](ctx, sb, query, builders...)
}
