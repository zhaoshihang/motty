package configstore

import (
	"context"
	"database/sql"
	"errors"
)

// ReadPreferences reads shared setting records and the local preferences document
// in one SQLite snapshot. Host, credential and other entity records stay unread.
func ReadPreferences(path string, format int) ([]Entry, []byte, error) {
	ctx := context.Background()
	db, err := openReadOnly(ctx, path, format)
	if err != nil {
		return nil, nil, err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	var local []byte
	err = tx.QueryRow("SELECT value FROM local WHERE key='state'").Scan(&local)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, err
	}
	rows, err := tx.Query("SELECT id, payload FROM records WHERE kind='setting' AND deleted=0")
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var settings []Entry
	for rows.Next() {
		entry := Entry{Kind: "setting"}
		if err := rows.Scan(&entry.ID, &entry.Payload); err != nil {
			return nil, nil, err
		}
		settings = append(settings, entry)
	}
	return settings, local, rows.Err()
}
