package bunpaginate

import (
	"context"
	"fmt"
	"math/big"
	"reflect"
	"strings"
	"time"

	libtime "github.com/formancehq/go-libs/v3/time"
	"github.com/uptrace/bun"
)

func UsingColumn[FILTERS any, ENTITY any](ctx context.Context,
	sb *bun.SelectQuery,
	query ColumnPaginatedQuery[FILTERS]) (*Cursor[ENTITY], error) {
	ret := make([]ENTITY, 0)

	sb = sb.Model(&ret)
	sb = sb.Limit(int(query.PageSize) + 1) // Fetch one additional item to find the next token
	order := query.Order
	if query.Reverse {
		order = order.Reverse()
	}
	sb = sb.OrderExpr(fmt.Sprintf("%s %s", query.Column, order))

	if query.PaginationID != nil {
		if query.Reverse {
			switch query.Order {
			case OrderAsc:
				sb = sb.Where(fmt.Sprintf("%s < ?", query.Column), query.PaginationID)
			case OrderDesc:
				sb = sb.Where(fmt.Sprintf("%s > ?", query.Column), query.PaginationID)
			}
		} else {
			switch query.Order {
			case OrderAsc:
				sb = sb.Where(fmt.Sprintf("%s >= ?", query.Column), query.PaginationID)
			case OrderDesc:
				sb = sb.Where(fmt.Sprintf("%s <= ?", query.Column), query.PaginationID)
			}
		}
	}

	if err := sb.Scan(ctx); err != nil {
		return nil, err
	}

	var v ENTITY
	fields := findPaginationFieldPath(v, query.Column)

	var (
		paginationIDs = make([]*big.Int, 0)
	)
	for _, t := range ret {
		paginationID := findPaginationField(t, fields...)
		if query.Bottom == nil {
			query.Bottom = paginationID
		}
		paginationIDs = append(paginationIDs, paginationID)
	}

	hasMore := len(ret) > int(query.PageSize)
	if hasMore {
		ret = ret[:len(ret)-1]
	}
	if query.Reverse {
		for i := 0; i < len(ret)/2; i++ {
			ret[i], ret[len(ret)-i-1] = ret[len(ret)-i-1], ret[i]
		}
	}

	var previous, next *ColumnPaginatedQuery[FILTERS]

	if query.Reverse {
		cp := query
		cp.Reverse = false
		next = &cp

		if hasMore {
			cp := query
			cp.PaginationID = paginationIDs[len(paginationIDs)-2]
			previous = &cp
		}
	} else {
		if hasMore {
			cp := query
			cp.PaginationID = paginationIDs[len(paginationIDs)-1]
			next = &cp
		}
		if query.PaginationID != nil {
			if (query.Order == OrderAsc && query.PaginationID.Cmp(query.Bottom) > 0) || (query.Order == OrderDesc && query.PaginationID.Cmp(query.Bottom) < 0) {
				cp := query
				cp.Reverse = true
				previous = &cp
			}
		}
	}

	return &Cursor[ENTITY]{
		PageSize: int(query.PageSize),
		HasMore:  next != nil,
		Previous: previous.EncodeAsCursor(),
		Next:     next.EncodeAsCursor(),
		Data:     ret,
	}, nil
}

func findPaginationFieldPath(v any, paginationColumn string) []reflect.StructField {

	typeOfT := reflect.TypeOf(v)
	for i := 0; i < typeOfT.NumField(); i++ {
		field := typeOfT.Field(i)
		fieldType := field.Type

		// If the field is a pointer, we unreference it to target the concrete type
		// For example:
		// type Object struct {
		//     *AnotherObject
		// }
		for {
			if field.Type.Kind() == reflect.Ptr {
				fieldType = field.Type.Elem()
			}
			break
		}

		switch fieldType.Kind() {
		case reflect.Struct:
			if fieldType.AssignableTo(reflect.TypeOf(time.Time{})) ||
				fieldType.AssignableTo(reflect.TypeOf(libtime.Time{})) ||
				fieldType.AssignableTo(reflect.TypeOf(big.Int{})) ||
				fieldType.AssignableTo(reflect.TypeOf(BigInt{})) {

				if fields := checkTag(field, paginationColumn); len(fields) > 0 {
					return fields
				}
			} else {
				fields := findPaginationFieldPath(reflect.New(fieldType).Elem().Interface(), paginationColumn)
				if len(fields) > 0 {
					return fields
				}
			}
		default:
			if fields := checkTag(field, paginationColumn); len(fields) > 0 {
				return fields
			}
		}
	}

	return nil
}

func checkTag(field reflect.StructField, paginationColumn string) []reflect.StructField {
	tag := field.Tag.Get("bun")
	column := strings.Split(tag, ",")[0]
	if column == paginationColumn {
		return []reflect.StructField{field}
	}

	return nil
}

func findPaginationField(v any, fields ...reflect.StructField) *big.Int {
	vOf := reflect.ValueOf(v)
	field := vOf.FieldByName(fields[0].Name)
	if len(fields) == 1 {
		switch rawPaginationID := field.Interface().(type) {
		case time.Time:
			return big.NewInt(rawPaginationID.UTC().UnixMicro())
		case *time.Time:
			return big.NewInt(rawPaginationID.UTC().UnixMicro())
		case *libtime.Time:
			return big.NewInt(rawPaginationID.UTC().UnixMicro())
		case libtime.Time:
			return big.NewInt(rawPaginationID.UTC().UnixMicro())
		case *BigInt:
			return (*big.Int)(rawPaginationID)
		case BigInt:
			return (*big.Int)(&rawPaginationID)
		case *big.Int:
			return rawPaginationID
		case big.Int:
			return &rawPaginationID
		case int64:
			return big.NewInt(rawPaginationID)
		case int:
			return big.NewInt(int64(rawPaginationID))
		case *int:
			return big.NewInt(int64(*rawPaginationID))
		case *int64:
			return big.NewInt(*rawPaginationID)
		default:
			panic(fmt.Sprintf("invalid paginationID, type %T not handled", rawPaginationID))
		}
	}

	return findPaginationField(v, fields[1:]...)
}
