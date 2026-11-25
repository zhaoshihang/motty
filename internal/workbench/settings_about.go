package workbench

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"sync"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/i18n"
	"motty/internal/update"
)

var hitAboutCopyInfo = tree.ID(settingsHits, "settings-about-copy")

// aboutBuildInfo is what this copy of the program is: its version, the platform
// it was built for, the Go runtime under it and the commit it came from.
type aboutBuildInfo struct {
	version     string
	versionText string // the version as the page shows it
	platform    string
	build       string // the commit and its date, or "" without one
	runtime     string
	copyText    string // the same facts as one block for a bug report
}

// aboutFacts reads the build once. The strings are made here, not on a paint,
// because a settings page repaints on every pointer move and must not allocate.
var aboutFacts = sync.OnceValue(func() aboutBuildInfo {
	info, _ := debug.ReadBuildInfo()
	return readAboutFacts(info)
})

func readAboutFacts(info *debug.BuildInfo) aboutBuildInfo {
	facts := aboutBuildInfo{
		version:  update.Current,
		platform: aboutPlatform(runtime.GOOS, runtime.GOARCH),
		runtime:  runtime.Version(),
	}
	facts.versionText = "v" + facts.version
	if info != nil {
		var revision, when string
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				revision = setting.Value
			case "vcs.time":
				when = setting.Value
			}
		}
		if len(revision) > 7 {
			revision = revision[:7]
		}
		if revision != "" {
			facts.build = revision
			if stamp, err := time.Parse(time.RFC3339, when); err == nil {
				facts.build += " · " + stamp.Format("2006-01-02")
			}
		}
	}
	facts.copyText = "Motty " + facts.version + "\nplatform: " + facts.platform + "\nruntime: " + facts.runtime + "\n"
	if facts.build != "" {
		facts.copyText += "build: " + facts.build + "\n"
	}
	return facts
}

// aboutPlatform names the operating system and architecture as a person reads
// them, while keeping the runtime's own names for anything unrecognised.
func aboutPlatform(goos, goarch string) string {
	system := goos
	switch goos {
	case "darwin":
		system = "macOS"
	case "windows":
		system = "Windows"
	case "linux":
		system = "Linux"
	case "freebsd":
		system = "FreeBSD"
	}
	switch goarch {
	case "amd64":
		goarch = "x86-64"
	case "386":
		goarch = "x86"
	}
	return system + " · " + goarch
}

// buildText names the build, with a word of its own when no commit was recorded.
func (facts aboutBuildInfo) buildText(m i18n.AboutMessages) string {
	if facts.build == "" {
		return m.BuildUnavailable
	}
	return facts.build
}

// Tiles are two to a row whenever the page is at least this wide; below it one
// tile per row keeps each one readable. The page width is known while it is
// declared, so the layout is chosen rather than approximated by a minimum cell.
const aboutTwoColumnMin = float32(460)

// tileBox is the inset grid of a group's card: rows of tiles with room between
// them. It takes the place of a row, so whatever follows it gets a hairline.
func (page *settingsPage) tileBox(children func()) {
	page.separator()
	ui.Column(page.c).FillWidth().Pad(14).Gap(10).Children(children)
}

// pair places two tiles side by side, or one above the other when the page is
// too narrow for two.
func (page *settingsPage) pair(first, second func()) {
	if page.width < aboutTwoColumnMin {
		ui.Column(page.c).FillWidth().Gap(10).Children(func() {
			first()
			second()
		})
		return
	}
	ui.Row(page.c).FillWidth().Gap(10).Children(func() {
		ui.Column(page.c).Grow(1).Basis(0).Children(first)
		ui.Column(page.c).Grow(1).Basis(0).Children(second)
	})
}

// factTile is one fact of the build: its name above its value.
func (page *settingsPage) factTile(icon uint8, label, value string) {
	c, pal := page.c, page.state.pal
	tile := ui.Column(c).FillWidth().Height(60).PadSides(10, 12, 10, 12).Justify(ui.Center).Gap(3).Background(pal.surfaceSoft).Radius(radius.item)
	page.matchAnchor(label, tile, 0)
	tile.Children(func() {
		ui.Row(c).Items(ui.Center).Gap(6).Children(func() {
			ui.Icon(c, lucideIcons[icon]).Size(14, 14).Color(pal.muted).Shrink(0)
			ui.Text(c, label).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(15)
		})
		ui.Text(c, value).Mono().FontSize(metrics.FontSizeM).Color(pal.text).LineHeight(19).Ellipsis()
	})
}

// paintAboutSettings shows the installed build and its update settings.
func (state *State) paintAboutSettings(page *settingsPage) {
	m, pal := state.messages, state.pal
	facts := aboutFacts()
	copyAction := m.About.CopyAction
	if state.settings.aboutCopied {
		copyAction = m.About.CopiedAction
	}
	c := page.c
	page.groups(func() {
		page.group("", func() {
			// The wordmark is a picture, so it keeps a slot the page paints
			// once it is laid out, as the sidebar header does.
			row := ui.Row(c).Height(96).PadX(18).Gap(16).Items(ui.Center)
			row.Children(func() {
				ui.Column(c).Grow(1).Children(func() {
					page.add(settingsControl{kind: controlLogo, slot: ui.Slot(c).Size(lockupWidth, lockupHeight).Shrink(0)})
					ui.Paragraph(c, m.About.Tagline, 2).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(16).MarginSides(6, 0, 0, 0)
				})
				page.button(hitAboutCopyInfo, copyAction, aiButtonOutline, false).Shrink(0)
			})
		})
		page.group(m.About.InfoTitle, func() {
			page.tileBox(func() {
				page.pair(func() {
					page.factTile(iconInfo, m.About.Version, facts.versionText)
				}, func() {
					page.factTile(iconDesktop, m.About.Platform, facts.platform)
				})
				page.pair(func() {
					page.factTile(iconWrench, m.About.Build, facts.buildText(m.About))
				}, func() {
					page.factTile(iconTerminal, m.About.Runtime, facts.runtime)
				})
			})
		})
		// The update controls change the copy the facts above describe, so they
		// sit with it rather than among the application's other preferences.
		page.group(m.About.UpdateTitle, func() {
			state.paintUpdateRows(page)
		})
	})
}

// clickAboutSettings handles the presses of the About page. It reports whether
// the identity was one of them.
func (state *State) clickAboutSettings(id gfx.ID) bool {
	switch id {
	case hitAboutCopyInfo:
		state.copyAboutInfo()
	default:
		return false
	}
	return true
}

// copyAboutInfo puts the build's facts on the clipboard, ready to paste into a
// bug report.
func (state *State) copyAboutInfo() {
	if err := state.options.SetClipboardText(aboutFacts().copyText); err != nil {
		state.settings.aboutCopied = false
		state.status = fmt.Sprintf(state.messages.StatusClipboardFailedFmt, err)
		return
	}
	state.settings.aboutCopied = true
	state.status = state.messages.About.Copied
}
