package repository

import "context"

// queryAll runs the query and returns scan's result for every row, in row order. It owns the rows:
// they are closed before it returns on every path, so the connection is back in the pool (the write
// pool has one) and the caller may issue the next query. A query, scan or iteration error is
// returned as it is; the caller adds the context.
func queryAll[T any](ctx context.Context, q queryer, scan func(rowScanner) (T, error), query string, args ...any) ([]T, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []T
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// scanString scans a result of one text column.
func scanString(row rowScanner) (string, error) {
	var s string
	return s, row.Scan(&s)
}
