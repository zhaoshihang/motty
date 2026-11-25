package workbench

import (
	"fmt"
	"path"
	"strings"

	"github.com/vivaui/viva/components"
	"motty/internal/sftpclient"
)

var hitSFTPRemoteEditor = tree.ID(sftpHits, "sftp-remote-editor")

// sftpEditorItem is the more-menu item that opens one folder in the chosen
// editor: its label, and whether it is disabled.
func (state *State) sftpEditorItem(view *sftpView, folder *sftpFolder) (string, bool) {
	m := state.messages.Tools
	name := state.defaultEditor.name(m)
	if name == "" {
		return "", false
	}
	disabled := folder.path == "" || view.editorResults != nil
	if folder != &view.lfolder {
		session, ok := state.sftpSession(view)
		disabled = disabled || !ok || !session.hasServer()
		if state.defaultEditor == editorCustom {
			disabled = true
			return fmt.Sprintf("%s — %s", fmt.Sprintf(m.FilesEditorOpenFmt, name), m.FilesCustomEditorRemoteOnly), true
		}
	}
	return fmt.Sprintf(m.FilesEditorOpenFmt, name), disabled
}

// editorRequestFor copies what the request needs out of the window, so the
// background reads nothing of it: the SSH config is read there, not here.
func (state *State) editorRequestFor(view *sftpView, folder *sftpFolder, session *Session) editorRequest {
	m := state.messages.Tools
	request := editorRequest{
		editor:    state.defaultEditor,
		directory: folder.path,
		local:     folder == &view.lfolder,
		portFmt:   m.FilesEditorPortFmt,
		jumpFmt:   m.FilesEditorJump,
	}
	if request.local {
		return request
	}
	host := session.host
	request.host = editorHost{
		address: host.Address, user: state.hostCatalog.login(host), systemUser: state.hostCatalog.systemUser,
		port: host.Port, mottyJump: hasMottyJump(state, host),
	}
	request.aliases, request.configPath = state.sshAliases(host), state.sshConfigPath()
	return request
}

// hasMottyJump reports whether Motty reaches the host through a jump host.
func hasMottyJump(state *State, host Host) bool {
	_, jumped := state.routeJump(host.ID)
	return jumped
}

// editorResult is one answer from the background.
type editorResult struct {
	editor    ExternalEditor
	directory string
	err       error
}

// openSFTPInEditor hands the folder the panel shows to the chosen editor, in the
// background, so a slow opener does not hold a paint.
func (state *State) openSFTPInEditor(view *sftpView) {
	state.openSFTPEditorPath(view, "")
}

func (state *State) canOpenVSCodeFile(view *sftpView, entry sftpclient.Entry) bool {
	if state.defaultEditor != editorVSCode || view.local || !entry.Mode.IsRegular() || entry.IsDir() || entry.LinkBroken {
		return false
	}
	// VS Code parses colons as line/column separators. A drive prefix in a
	// Windows SFTP path is fine. Use the native editor for ambiguous names.
	location := path.Join(view.path, entry.Name)
	if len(location) >= 4 && location[0] == '/' && location[2:4] == ":/" {
		location = location[3:]
	}
	return !strings.Contains(location, ":")
}

func (state *State) openSelectedInRemoteEditor(view *sftpView) {
	entry, ok := state.selectedSFTPEntry(view)
	if !ok || view.busy || !state.canOpenVSCodeFile(view, entry) {
		return
	}
	state.openSFTPEditorPath(view, entry.Name)
}

func (state *State) openSFTPEditorPath(view *sftpView, fileName string) {
	folder := view.active()
	label, disabled := state.sftpEditorItem(view, folder)
	session, ok := state.sftpSession(view)
	if label == "" || disabled || !ok {
		return
	}
	request := state.editorRequestFor(view, folder, session)
	if fileName != "" {
		request.directory = path.Join(folder.path, fileName)
	}
	open := state.options.OpenSchemeURL
	customExecutable := state.customEditorPath
	launchCustom := state.options.LaunchCustomEditor
	if request.editor == editorCustom && !validCustomEditorPath(customExecutable) {
		state.status = state.messages.Tools.FilesCustomEditorInvalid
		return
	}
	results, notify := make(chan editorResult, 1), state.notifyForView(session, view)
	view.editorResults = results
	go func() {
		var err error
		if request.editor == editorCustom {
			err = launchCustom(customExecutable, request.directory)
		} else {
			err = request.open(open, fileName != "")
		}
		results <- editorResult{editor: request.editor, directory: request.directory, err: err}
		notify()
	}()
}

// pumpSFTPEditor takes the background's answer, if it has arrived.
func (state *State) pumpSFTPEditor(session *Session, view *sftpView) {
	if view.editorResults == nil {
		return
	}
	select {
	case result := <-view.editorResults:
		view.editorResults = nil
		if result.err != nil && session.files == view {
			m := state.messages.Tools
			title := fmt.Sprintf(m.FilesEditorOpenFailedTitleFmt, result.editor.name(m))
			state.fileOpenPathReason(view.detached, title, m.FilesEditorOpenFailedFmt, result.directory,
				firstLines(result.err.Error(), 3), components.ToastError)
		}
	default:
	}
}
