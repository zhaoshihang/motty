package workbench

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"slices"
	"strings"

	"motty/internal/backup"
	"motty/internal/configstore"
)

// Keep this an explicit list. The local document also has open sessions, recent
// hosts, sync credentials, user commands and references to notes and SSH keys.
// Decoding each value through its app type also excludes unknown nested fields.
var backupPreferenceTypes = func() map[string]reflect.Type {
	names := strings.Fields(`
		language theme icon_color corners background_output_alerts tab_orientation tab_double_click
		tab_numbers hidden_nav vertical_tabs_auto_hide vertical_tabs_grouping desktop_fullscreen_bar_auto_hide
		recent_connect_sort local_shells_expanded local_shortcuts local_shell hidden_shells local_start
		network_proxy ssh_agent terminalOption terminal_font_size interface_text_size interface_font
		interface_font_main_weight interface_font_bold_weight terminal_font_face terminal_font_family
		terminal_font_main_weight terminal_font_bold_weight terminal_font_fallbacks terminal_find terminal_scrollback
		terminal_cursor connection_timeout_seconds connection_keepalive_seconds connection_keepalive_off
		connection_reconnect restore_connect share_connections startup_local startup_shell startup_disabled startup_reuse
		terminal_clipboard terminal_cursor_steady terminal_cursor_thickness terminal_copy_on_select
		terminal_word_separators terminal_right_click_paste tab_title_mode terminal_tab_title terminal_show_latency
		terminal_toolbar_position terminal_toolbar_size terminal_bell_off terminal_paste_confirm_off
		terminal_input_click_off terminal_link_plain_click ai_panel_width ai_composer_height monitor_panel_width
		terminal_scheme terminal_scheme_dark terminal_scheme_light terminal_padding terminal_gutter terminal_intense
		terminal_line_height terminal_notifications terminal_paste_image terminal_password_hint terminal_close_confirm
		close_window_confirm sftp_parallel sftp_keep_times sftp_follow sftp_terminal_follow sftp_path_input
		sftp_panel_auto_open sftp_directory_tree sftp_panel_side sftp_panel_width sftp_window_order sftp_open_mode
		file_preview_mb sftp_columns sftp_download_directory sftp_transfer_filter default_editor custom_editor_path
		toolbars update_check update_source update_address close_action keep_windows_on_exit relaunch_action
		ssh_browser highlight_builtin command_history command_suggest_off command_accept_hint_off
		command_completion_mode command_inline_suggest_off session_logs_off shortcut_version
		shortcut_defaults_generation shortcuts host_view host_order host_side_width host_tree_open hosts_panel_open
		hosts_panel_width host_privacy host_privacy_show_name host_privacy_show_address host_privacy_show_user
		commands_shown commands_side commands_sort commands_width
	`)
	types := make(map[string]reflect.Type, len(names))
	for _, field := range reflect.VisibleFields(reflect.TypeFor[savedState]()) {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if slices.Contains(names, name) {
			types[name] = field.Type
		}
	}
	if len(types) != len(names) {
		panic("settings backup references an unknown saved preference")
	}
	return types
}()

func preferenceValue(name string, raw json.RawMessage) (json.RawMessage, error) {
	kind, allowed := backupPreferenceTypes[name]
	if !allowed {
		return nil, fmt.Errorf("unsupported backup preference %q", name)
	}
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return json.RawMessage("null"), nil
	}
	value := reflect.New(kind)
	if err := json.Unmarshal(raw, value.Interface()); err != nil {
		return nil, fmt.Errorf("invalid backup preference %q", name)
	}
	if name == "update_address" {
		// Private feed URLs can carry credentials in the query or userinfo.
		address, err := url.Parse(value.Elem().String())
		if err != nil || address.User != nil || address.RawQuery != "" || address.Fragment != "" {
			return nil, nil
		}
	}
	return json.Marshal(value.Interface())
}

func readPreferenceValues(db string) (backup.Settings, error) {
	records, local, err := configstore.ReadPreferences(db, configFormat)
	if err != nil {
		return nil, err
	}
	fields := make(backup.Settings)
	if len(local) > 0 {
		if err := json.Unmarshal(local, &fields); err != nil || fields == nil {
			return nil, errors.New("invalid local settings document")
		}
	}
	for _, record := range records {
		if backupPreferenceTypes[record.ID] == nil || !syncedSettings[record.ID] {
			continue
		}
		var payload recordPayload
		if err := json.Unmarshal(record.Payload, &payload); err != nil {
			return nil, err
		}
		fields[record.ID] = payload.Data
	}
	settings := backup.Settings{}
	for name := range backupPreferenceTypes {
		raw, err := preferenceValue(name, fields[name])
		if err != nil {
			return nil, err
		}
		if raw != nil {
			settings[name] = raw
		}
	}
	return settings, nil
}

func restorePreferenceValues(db string, settings backup.Settings) error {
	for name, raw := range settings {
		clean, err := preferenceValue(name, raw)
		if err != nil || clean == nil {
			return fmt.Errorf("invalid settings backup field %q", name)
		}
		settings[name] = clean
	}
	store, err := configstore.Open(db, configFormat)
	if err != nil {
		return err
	}
	defer store.Close()
	entries, local, err := configstore.ReadPreferences(db, configFormat)
	if err != nil {
		return err
	}
	fields := backup.Settings{}
	if len(local) != 0 {
		if err := json.Unmarshal(local, &fields); err != nil || fields == nil {
			return errors.New("invalid destination settings document")
		}
	}
	change := configstore.Change{Writer: configWriter(), Expected: map[configstore.Key][]byte{},
		ExpectedLocal: map[string][]byte{localKey: local}}
	current := map[string][]byte{}
	for _, entry := range entries {
		current[entry.ID] = entry.Payload
	}
	for name, value := range settings {
		delete(fields, name)
		reset := bytes.Equal(value, []byte("null"))
		if !syncedSettings[name] {
			if !reset {
				fields[name] = value
			}
			continue
		}
		key := configstore.Key{Kind: kindSetting, ID: name}
		change.Expected[key] = current[name]
		if reset {
			if current[name] != nil {
				change.Delete = append(change.Delete, key)
			}
			continue
		}
		payload, err := json.Marshal(recordPayload{Data: value})
		if err != nil {
			return err
		}
		change.Put = append(change.Put, configstore.Entry{Kind: kindSetting, ID: name, Payload: payload})
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	change.Local = map[string][]byte{localKey: encoded}
	_, err = store.Commit(change)
	return err
}

// SettingsBackup runs the operations used by the Gist backup script.
// The verify operation compares all preferences, including shortcut shells and directories.
func SettingsBackup(action, db string, sealed []byte, password string) ([]byte, string, error) {
	if action == "export" {
		values, err := readPreferenceValues(db)
		if err != nil {
			return nil, "", err
		}
		out, err := backup.SealSettings(values, password)
		return out, "", err
	}
	values, err := backup.OpenSettings(sealed, password)
	if err != nil {
		return nil, "", err
	}
	if action == "check" {
		return nil, "settings snapshot decrypted", nil
	}
	if action == "restore" {
		if err := restorePreferenceValues(db, values); err != nil {
			return nil, "", err
		}
	} else if action != "verify" {
		return nil, "", errors.New("unknown settings backup operation")
	}
	actual, err := readPreferenceValues(db)
	if err != nil {
		return nil, "", err
	}
	count := 0
	for name, want := range values {
		if !bytes.Equal(actual[name], want) {
			return nil, "", fmt.Errorf("backup differs from the database at %q", name)
		}
		if string(want) != "null" && name != "local_shortcuts" {
			count++
		}
	}
	if action == "verify" && len(actual) != len(values) {
		return nil, "", errors.New("settings snapshot contains a different set of preferences")
	}
	var shortcuts map[string]localShortcut
	if raw := values["local_shortcuts"]; raw != nil {
		if err := json.Unmarshal(raw, &shortcuts); err != nil {
			return nil, "", err
		}
	}
	return nil, fmt.Sprintf("%d settings, %d local shortcuts", count, len(shortcuts)), nil
}
