package workbench

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/configstore"
	"motty/internal/localshell"
)

// The map key is the user-visible name. Each value pins the shell by its
// executable path (or WSL distribution), independently of the default shell.
type localShortcut struct {
	Directory string `json:"directory"`
	Shell     string `json:"shell"`
}

func (state *State) localShortcutShellLabel(program string) string {
	for _, choice := range state.localShellMenu.choices {
		if choice.Path == program {
			return choice.Name
		}
	}
	if distro, ok := localshell.WSLDistro(program); ok {
		return localshell.WSLName(distro)
	}
	if program == "" {
		return state.messages.SettingsLocalShellEmpty
	}
	return filepath.Base(program)
}

func (state *State) addLocalShortcutRows() {
	palette := &state.commandPalette
	for _, name := range slices.Sorted(maps.Keys(state.localShortcuts)) {
		shortcut := state.localShortcuts[name]
		detail := state.localShortcutShellLabel(shortcut.Shell) + " · " + shortcut.Directory
		index := len(palette.entries)
		palette.addDetailed(name, detail, hitConnectShortcuts.At(index))
		palette.entries[index].hint = detail
	}
}

func (state *State) localShortcutProblem(shortcut localShortcut) string {
	m := state.messages.LocalShortcuts
	if !validLocalShell(shortcut.Shell) {
		return m.ShellUnavailable
	}
	distro, wsl := localshell.WSLDistro(shortcut.Shell)
	if wsl {
		if state.localShellMenu.listing == nil && !state.shellListed(shortcut.Shell) {
			return m.ShellUnavailable
		}
		if _, reachable := localshell.WSLPath(shortcut.Directory, distro); !reachable {
			return m.DirectoryUnavailable
		}
	} else {
		if _, err := exec.LookPath(shortcut.Shell); err != nil {
			return m.ShellUnavailable
		}
		if localshell.IsWSLUNC(shortcut.Directory) {
			return m.DirectoryUnavailable
		}
	}
	if !validLocalDirectory(shortcut.Directory) {
		return m.DirectoryUnavailable
	}
	if info, err := os.Stat(shortcut.Directory); err != nil || !info.IsDir() {
		return m.DirectoryUnavailable
	}
	return ""
}

func (state *State) launchLocalShortcut(name string) {
	shortcut, ok := state.localShortcuts[name]
	if !ok {
		state.rebuildCommandPalette()
		return
	}
	if message := state.localShortcutProblem(shortcut); message != "" {
		state.editLocalShortcut(name)
		state.form.dialogError = message
		return
	}
	state.closeCommandPalette()
	state.pendingWorkspace, state.pendingPane, state.pendingDirectory = 0, 0, shortcut.Directory
	state.openLocalTerminalWith(localshell.Choice{Name: name, Path: shortcut.Shell})
}

func (state *State) showLocalShortcuts() {
	state.openConnectPickerTab(connectSortShortcuts)
}

func (state *State) editLocalShortcut(originalName string) {
	original, found := state.localShortcuts[originalName]
	if originalName != "" && !found {
		state.rebuildCommandPalette()
		return
	}
	state.closeCommandPalette()
	m := state.messages
	program := original.Shell
	dialog := formDialog{title: m.LocalShortcuts.Add, submit: m.SettingsLocalStartupSave, cancel: (*State).showLocalShortcuts}
	if originalName == "" {
		program = state.localShellSettingChoice().Path
	} else {
		dialog.title = m.LocalShortcuts.Edit
		dialog.backLabel = m.SFTPDelete
		dialog.back = func(s *State, draft *formDialog) {
			s.openForm(formDialog{
				title: fmt.Sprintf(m.LocalShortcuts.DeleteTitleFmt, originalName), submit: m.SFTPDelete, danger: true,
				cancel: func(s *State) { s.openForm(*draft, draft.fields[:draft.count]...) },
				run: func(s *State, _ *formDialog) string {
					return s.storeLocalShortcut(originalName, original, "", localShortcut{})
				},
			})
		}
	}
	dialog.run = func(s *State, form *formDialog) string {
		name := strings.TrimSpace(form.value(0))
		if name == "" || strings.ContainsFunc(name, unicode.IsControl) {
			return m.LocalShortcuts.NameRequired
		}
		shortcut := localShortcut{Directory: strings.TrimSpace(form.value(1)), Shell: program}
		if message := s.localShortcutProblem(shortcut); message != "" {
			return message
		}
		shortcut.Directory = filepath.Clean(shortcut.Directory)
		return s.storeLocalShortcut(originalName, original, name, shortcut)
	}
	form := state.openForm(dialog, formField{label: m.ColumnName},
		formField{label: m.LocalShortcuts.Directory, browseDirectory: m.Tools.TransferChooseFolder},
		formField{label: m.LocalShortcuts.Shell, control: func(c ui.C, id gfx.ID) {
			state.adoptWSLDistros()
			var paths []string
			for _, choice := range state.localShellMenu.choices {
				if !state.shellHidden(choice) || choice.Path == program {
					paths = append(paths, choice.Path)
				}
			}
			ui.SelectID(c, id, &program, paths, state.localShortcutShellLabel).Label(m.LocalShortcuts.Shell).
				Placeholder(state.localShortcutShellLabel(program)).FillWidth().Disabled(len(paths) == 0)
		}},
	)
	form.set(0, originalName)
	form.set(1, original.Directory)
}

// An empty name deletes the entry. Keep the user's draft open on failure,
// and compare against the entry the editor originally showed on a retry.
func (state *State) storeLocalShortcut(originalName string, original localShortcut, name string, shortcut localShortcut) string {
	m := &state.messages
	for attempt := 0; ; attempt++ {
		// The editor may have stayed open across another window's write. Build
		// the rename or deletion on the latest map and reject a stale original.
		if state.config != nil && !state.saveBlocked {
			if err := state.reloadSettings(); err != nil {
				return fmt.Sprintf(m.StatusSaveFailedFmt, err)
			}
		}
		current := state.localShortcuts
		if value, ok := current[originalName]; originalName != "" && (!ok || value != original) {
			return m.LocalShortcuts.Changed
		}
		if _, exists := current[name]; name != "" && name != originalName && exists {
			return m.LocalShortcuts.NameTaken
		}
		next := maps.Clone(current)
		delete(next, originalName)
		if name != "" {
			if next == nil {
				next = make(map[string]localShortcut)
			}
			next[name] = shortcut
		}
		state.localShortcuts = next
		err := state.saveWithResultModeRecovery(true, false, true)
		if err == nil {
			state.closeModal()
			state.showLocalShortcuts()
			return ""
		}
		state.localShortcuts = current
		if attempt == 0 && errors.Is(err, configstore.ErrConflict) {
			continue
		}
		return fmt.Sprintf(m.StatusSaveFailedFmt, err)
	}
}
