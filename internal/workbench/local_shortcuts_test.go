//go:build darwin || windows || linux

package workbench

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"motty/internal/localshell"
)

func TestLocalShortcutDirectoryPickerSurvivesRepaint(t *testing.T) {
	state := newTestState(t, testOptions(t))
	view := openPickerView(t, state, 1)
	state.editLocalShortcut("")
	state.form.set(0, "draft")
	state.form.set(1, "typed path")
	label := state.messages.Tools.TransferChooseFolder
	dir := t.TempDir()
	failure := errors.New("folder picker failed")
	for _, step := range []struct {
		path, wantDirectory, wantError string
		err                            error
	}{
		{err: app.ErrCanceled, wantDirectory: "typed path"},
		{path: dir, wantDirectory: dir},
		{err: app.ErrCanceled, wantDirectory: dir},
		{err: failure, wantDirectory: dir, wantError: failure.Error()},
	} {
		calls := 0
		state.options.OpenDirectory = func(options app.OpenDirectoryOptions) (string, error) {
			calls++
			if calls > 1 {
				t.Fatal("repainting the open picker delivered the folder click again")
			}
			if options.Title != label {
				t.Fatalf("picker title = %q, want %q", options.Title, label)
			}
			// The native Windows picker pumps window messages while it is open.
			if err := view.Frame(); err != nil {
				t.Fatal(err)
			}
			return step.path, step.err
		}
		if err := view.Frame(); err != nil {
			t.Fatal(err)
		}
		clickFormLabel(t, view, gfx.RoleButton, label)
		if calls != 1 || state.modal != modalForm || state.form.value(0) != "draft" ||
			state.form.value(1) != step.wantDirectory || state.form.dialogError != step.wantError {
			t.Fatalf("picker calls=%d, modal=%v, name=%q, directory=%q, error=%q",
				calls, state.modal, state.form.value(0), state.form.value(1), state.form.dialogError)
		}
	}

	state.options.OpenDirectory = func(app.OpenDirectoryOptions) (string, error) {
		state.editLocalShortcut("")
		state.form.set(1, "another draft")
		return dir, nil
	}
	clickFormLabel(t, view, gfx.RoleButton, label)
	if got := state.form.value(1); got != "another draft" {
		t.Fatalf("the old picker overwrote the new draft with %q", got)
	}
}

func TestLocalShortcutPickerMaintainsAndLaunchesSavedEntries(t *testing.T) {
	first, second := "sh", "bash"
	if runtime.GOOS == "windows" {
		first, second = "powershell.exe", "cmd.exe"
	}
	firstPath, err := exec.LookPath(first)
	if err != nil {
		t.Skip(err)
	}
	shell, err := exec.LookPath(second)
	if err != nil {
		t.Skip(err)
	}
	dir := filepath.Join(t.TempDir(), "项目 A & B")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	options := testOptions(t)
	state := newTestState(t, options)
	m := state.messages
	choices := []localshell.Choice{{Name: first, Path: firstPath, Default: true}, {Name: second, Path: shell}}
	withShellChoices(t, state, choices, nil)
	state.Click(hitNewTab)
	view := openPickerView(t, state, 1)
	clickLabel := func(label string) {
		t.Helper()
		if err := view.ClickLabel(label); err != nil {
			t.Fatal(err)
		}
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
	}
	clickLabel(m.NewTabSortRecent)
	clickLabel(m.LocalShortcuts.Tab)
	if !state.recentConnectSort || len(state.commandPalette.visible) != 0 {
		t.Fatal("the quick tab mixed in recent hosts or changed the host sort")
	}
	clickFormHit(t, view, hitConnectShortcutNew)
	clickFormHit(t, view, hitFormFieldList.At(0))
	if err := view.Type("project"); err != nil {
		t.Fatal(err)
	}
	clickFormHit(t, view, hitFormFieldList.At(1))
	if err := view.Type(dir); err != nil {
		t.Fatal(err)
	}
	clickFormHit(t, view, hitFormFieldList.At(2))
	clickLabel(second)
	clickFormHit(t, view, hitFormSubmit)
	want := localShortcut{Directory: dir, Shell: shell}
	if state.modal != modalCommandPalette || state.localShortcuts["project"] != want {
		t.Fatalf("save: row=%+v modal=%v error=%q", state.localShortcuts["project"], state.modal, state.form.dialogError)
	}

	// The row is read back from disk, and a change in discovery order cannot
	// make its saved executable mean another shell.
	reopened := newTestState(t, options)
	withShellChoices(t, reopened, []localshell.Choice{choices[1], choices[0]}, nil)
	reopened.Click(hitNewTab)
	clickSettingsChoice(t, reopened, m.LocalShortcuts.Tab)
	reopened.commandPalette.search.Insert(second + " B")
	reopened.refreshCommandPalette()
	if len(reopened.commandPalette.visible) != 1 {
		t.Fatal("the saved shell name and directory did not match the search")
	}
	result := reopened.Key(gfx.KeyEvent{Target: hitCommandPaletteSearch, Key: gfx.KeyEnter})
	session, ok := reopened.sessionByIDPointer(reopened.activeSessionID)
	if !result.Handled || !ok || !session.isLocal() || localShellOf(t, session) != shell || session.localDirectory != dir || session.Title != "project" {
		t.Fatal("Enter did not open the saved shell and directory in its own named tab")
	}

	clickFormHit(t, view, hitConnectShortcutEdit.At(0))
	editedDir := t.TempDir()
	state.form.set(0, "renamed")
	state.form.set(1, editedDir)
	clickFormHit(t, view, hitFormSubmit)
	want.Directory = editedDir
	if len(state.localShortcuts) != 1 || state.localShortcuts["renamed"] != want {
		t.Fatal("renaming duplicated or lost the shortcut")
	}
	if err := os.Remove(editedDir); err != nil {
		t.Fatal(err)
	}
	tabs := len(state.deck.Workspaces(nil))
	clickLabel("renamed")
	if state.modal != modalForm || state.form.dialogError != m.LocalShortcuts.DirectoryUnavailable || len(state.deck.Workspaces(nil)) != tabs {
		t.Fatal("opening a missing directory did not offer to repair the shortcut")
	}
	clickFormHit(t, view, hitFormBack)
	clickFormHit(t, view, hitFormCancel)
	if state.form.title != m.LocalShortcuts.Edit {
		t.Fatal("canceling deletion did not return to the editor")
	}
	clickFormHit(t, view, hitFormBack)
	clickFormHit(t, view, hitFormSubmit)
	if state.modal != modalCommandPalette || len(newTestState(t, options).localShortcuts) != 0 {
		t.Fatal("deletion did not survive reopening the configuration")
	}
}

func TestLocalShortcutEditsInTwoWindowsKeepOtherEntriesAndDetectStaleEdits(t *testing.T) {
	options := testOptions(t)
	a, b := newTestState(t, options), newTestState(t, options)
	entry := localShortcut{Directory: t.TempDir(), Shell: "shell-a"}
	edited := localShortcut{Directory: t.TempDir(), Shell: "shell-b"}
	if problem := a.storeLocalShortcut("", localShortcut{}, "first", entry); problem != "" {
		t.Fatal(problem)
	}
	if problem := b.storeLocalShortcut("", localShortcut{}, "second", entry); problem != "" {
		t.Fatal(problem)
	}
	if problem := b.storeLocalShortcut("second", entry, "first", edited); problem != b.messages.LocalShortcuts.NameTaken {
		t.Fatalf("duplicate name: %q", problem)
	}
	if problem := a.storeLocalShortcut("first", entry, "first", edited); problem != "" {
		t.Fatal(problem)
	}
	if problem := b.storeLocalShortcut("first", entry, "", localShortcut{}); problem != b.messages.LocalShortcuts.Changed {
		t.Fatalf("stale deletion: %q", problem)
	}
	saved := newTestState(t, options).localShortcuts
	if len(saved) != 2 || saved["first"] != edited || saved["second"] != entry {
		t.Fatalf("saved shortcuts: %+v", saved)
	}
}
