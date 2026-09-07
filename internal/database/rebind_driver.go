package database

import (
	"context"
	"database/sql/driver"
	"fmt"
)

// rebindDriver wraps a database/sql driver (pgx in practice) rewriting every
// query's `?`/`?N` placeholders to `$N` before it reaches the engine. This
// lets ALL existing query code — sqlc-generated sqlite dialect plus hand
// SQL — run unchanged on Postgres. SQLite accepts `$N` natively so the same
// query text also runs there; the wrapper is only installed for postgres.
//
// Only the fast paths are intercepted (QueryerContext/ExecerContext, which
// database/sql prefers for Query/Exec on both DB and Tx handles).
// Explicit Prepare calls must Rebind first (see Rebind); preparing a
// `?`-style query on postgres fails at the server.
type rebindDriver struct {
	parent driver.Driver
}

// WrapRebind returns a driver that rebinds queries before delegating.
func WrapRebind(parent driver.Driver) driver.Driver {
	return &rebindDriver{parent: parent}
}

func (d *rebindDriver) Open(name string) (driver.Conn, error) {
	c, err := d.parent.Open(name)
	if err != nil {
		return nil, err
	}
	return &rebindConn{Conn: c}, nil
}

type rebindConn struct {
	driver.Conn
}

func rebindQuery(query string) (string, error) {
	r, err := Rebind(query)
	if err != nil {
		// Placeholder-free queries (DDL probes, SELECT 1) pass through.
		return query, nil
	}
	return r, nil
}

func (c *rebindConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	q, err := rebindQuery(query)
	if err != nil {
		return nil, err
	}
	if qer, ok := c.Conn.(driver.QueryerContext); ok {
		return qer.QueryContext(ctx, q, args)
	}
	return nil, fmt.Errorf("database: underlying driver lacks QueryerContext")
}

func (c *rebindConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	q, err := rebindQuery(query)
	if err != nil {
		return nil, err
	}
	if ex, ok := c.Conn.(driver.ExecerContext); ok {
		return ex.ExecContext(ctx, q, args)
	}
	return nil, fmt.Errorf("database: underlying driver lacks ExecerContext")
}

func (c *rebindConn) Prepare(query string) (driver.Stmt, error) {
	q, err := rebindQuery(query)
	if err != nil {
		return nil, err
	}
	return c.Conn.Prepare(q)
}

func (c *rebindConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	q, err := rebindQuery(query)
	if err != nil {
		return nil, err
	}
	if pc, ok := c.Conn.(driver.ConnPrepareContext); ok {
		return pc.PrepareContext(ctx, q)
	}
	return c.Conn.Prepare(q)
}
