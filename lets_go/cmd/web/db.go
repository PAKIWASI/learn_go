package main

import (
	"context"
	"database/sql"
	"time"
)


func openDB(dsn string) (*sql.DB, error) {
    db, err := sql.Open("pgx", dsn)
    if err != nil {
        return nil, err
    }

    // Connection pool settings (tune as you like)
    db.SetMaxOpenConns(25)
    db.SetMaxIdleConns(25)
    db.SetConnMaxIdleTime(5 * time.Minute)

    // Verify the connection actually works
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    if err = db.PingContext(ctx); err != nil {
        return nil, err
    }
    return db, nil
}
