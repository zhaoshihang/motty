package i18n

// AboutMessages are the texts of the About page of the settings: the
// application's identity and its updates.
type AboutMessages struct {
	Title string // the settings category, and the page's title

	Tagline string

	// The facts of the build. BuildUnavailable stands in for the commit when
	// this copy was built without one.
	InfoTitle        string
	Version          string
	Platform         string
	Build            string
	BuildUnavailable string
	Runtime          string

	// The update controls sit beside those facts, because they act on the copy
	// the facts describe.
	UpdateTitle string

	// Copying those facts for a bug report.
	CopyRow         string
	CopyDescription string
	CopyAction      string
	CopiedAction    string // the button's label once the facts are on the clipboard
	Copied          string
}
