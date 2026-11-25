package workbench

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"motty/internal/backup"
	"motty/internal/configstore"
)

func TestSettingsBackupRoundTripPreservesUnselectedData(t *testing.T) {
	const password = "test-only password"
	seed := func(name, local, setting string) string {
		t.Helper()
		db := filepath.Join(t.TempDir(), "config.db")
		store, err := configstore.Open(db, configFormat)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		_, err = store.Commit(configstore.Change{
			Local: map[string][]byte{localKey: []byte(local)},
			Put: []configstore.Entry{
				{Kind: kindSetting, ID: "terminal_scrollback", Payload: []byte(setting)},
				{Kind: kindHost, ID: name, Payload: []byte(`{"d":{"address":"private-host"}}`)},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		return db
	}
	source := seed("source", `{"language":"zh-CN","local_shortcuts":{"demo":{"directory":"example","shell":"bash","extra":"private-extra"}},"sync":{"token":"private-token"},"recent_connections":{"private-host":"yesterday"},"update_address":"https://example.org/feed?token=private-token"}`, `{"d":8000}`)
	target := seed("target", `{"language":"en","theme":"dark","sync":{"token":"target-token"},"update_address":"https://chosen.example/feed"}`, `{"d":2000}`)
	sealed, _, err := SettingsBackup("export", source, nil, password)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backup.Open(sealed, password); !errors.Is(err, backup.ErrNotBackup) {
		t.Fatal("a full restore accepted a preferences snapshot", err)
	}
	if _, _, err := SettingsBackup("restore", target, sealed, "another password"); !errors.Is(err, backup.ErrWrongPassword) {
		t.Fatal("wrong password", err)
	}
	values, err := backup.OpenSettings(sealed, password)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(values)
	if bytes.Contains(raw, []byte("private-")) || values["update_address"] != nil || string(values["terminal_scrollback"]) != "8000" {
		t.Fatal("invalid preference selection")
	}
	if _, _, err := SettingsBackup("verify", source, sealed, password); err != nil {
		t.Fatal(err)
	}
	if _, _, err := SettingsBackup("restore", target, sealed, password); err != nil {
		t.Fatal(err)
	}
	entries, local, err := configstore.ReadOnly(target, configFormat)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(local[localKey], &fields); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].ID != "target" || string(fields["sync"]) != `{"token":"target-token"}` ||
		string(fields["update_address"]) != `"https://chosen.example/feed"` || fields["theme"] != nil ||
		string(fields["local_shortcuts"]) != `{"demo":{"directory":"example","shell":"bash"}}` {
		t.Fatal("restore did not preserve the expected target data")
	}
}
