package workbench

import (
	"runtime/debug"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/i18n"
	"motty/internal/update"
)

func TestAboutBuildMetadataAndMissingInformation(t *testing.T) {
	for _, test := range []struct {
		name  string
		info  *debug.BuildInfo
		build string
	}{
		{name: "no build info"},
		{name: "no VCS settings", info: &debug.BuildInfo{}},
		{name: "release", info: &debug.BuildInfo{Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "429c5f54ea0b283e40c8dcfd7bc0d19300ba1fc8"},
			{Key: "vcs.time", Value: "2026-10-28T07:55:15Z"},
		}}, build: "429c5f5 · 2026-10-28"},
		{name: "revision without a date", info: &debug.BuildInfo{Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "123456789"},
			{Key: "vcs.time", Value: "invalid"},
		}}, build: "1234567"},
	} {
		t.Run(test.name, func(t *testing.T) {
			facts := readAboutFacts(test.info)
			if facts.build != test.build {
				t.Fatalf("build=%q, want %q", facts.build, test.build)
			}
			for _, language := range []i18n.Language{i18n.English, i18n.Chinese} {
				want := test.build
				if want == "" {
					want = i18n.For(language).About.BuildUnavailable
				}
				if got := facts.buildText(i18n.For(language).About); got != want {
					t.Fatalf("build text=%q, want %q", got, want)
				}
			}
			if test.build != "" && !strings.Contains(facts.copyText, "build: "+test.build+"\n") {
				t.Fatal("copied information lost the build identity")
			}
		})
	}
}

func TestAboutShowsBuildAndUpdates(t *testing.T) {
	state := newTestState(t, testOptions(t))
	var copied string
	state.options.SetClipboardText = func(value string) error { copied = value; return nil }

	state.Click(hitSettings)
	state.selectSettingsCategory(settingsAbout)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 1200})
	var texts []string
	for _, text := range canvas.Texts() {
		texts = append(texts, text.Value)
	}

	about := state.messages.About
	if state.settingsCategoryName(settingsAbout) != about.Title {
		t.Fatalf("the category is named %q", state.settingsCategoryName(settingsAbout))
	}
	facts := aboutFacts()
	tools := state.messages.Tools
	for _, want := range []string{about.Tagline, about.InfoTitle, about.UpdateTitle, tools.GeneralUpdateStartup, tools.GeneralUpdate, facts.platform, facts.runtime} {
		if !contains(texts, want) {
			t.Errorf("the About page does not paint %q", want)
		}
	}
	for _, id := range []gfx.ID{hitAboutCopyInfo, hitSettingsUpdateStartToggle, hitSettingsUpdateCheck} {
		if _, ok := hitByID(&canvas, id); !ok {
			t.Errorf("the About page does not paint %d", id)
		}
	}

	state.Click(hitAboutCopyInfo)
	for _, want := range []string{"Motty " + facts.version, "platform: " + facts.platform, "runtime: " + facts.runtime} {
		if !strings.Contains(copied, want) {
			t.Errorf("the copied version info %q does not name %q", copied, want)
		}
	}
	if state.status != about.Copied {
		t.Errorf("copy status = %q", state.status)
	}
	canvas = gfx.Canvas{}
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 1200})
	if hit, ok := hitByID(&canvas, hitAboutCopyInfo); !ok || hit.Label != about.CopiedAction {
		t.Errorf("the copy button says %q after copying, want %q", hit.Label, about.CopiedAction)
	}
	// A new visit to the page asks again with the plain label.
	state.selectSettingsCategory(settingsGeneral)
	state.selectSettingsCategory(settingsAbout)
	canvas = gfx.Canvas{}
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 1200})
	if hit, ok := hitByID(&canvas, hitAboutCopyInfo); !ok || hit.Label != about.CopyAction {
		t.Errorf("the copy button says %q on a new visit, want %q", hit.Label, about.CopyAction)
	}

	for _, removed := range []string{"Community", "Project", "QQ group", "Discord group", "Source repository"} {
		if contains(texts, removed) {
			t.Errorf("removed About section or link still visible: %q", removed)
		}
	}
}

func TestCustomUpdateAddressCanBeTypedSavedAndReopened(t *testing.T) {
	options := testOptions(t)
	options.CheckUpdate = func() (update.Release, error) { return update.Release{}, update.ErrNoRelease }
	state := newTestState(t, options)
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsAbout)
	m := state.messages.Tools
	if state.update.source != update.SourceCustom || state.update.address != update.DefaultCustomAddress || state.update.addressDraft != state.update.address {
		t.Fatalf("initial source %v, address %q, draft %q", state.update.source, state.update.address, state.update.addressDraft)
	}
	driver := labelDriver(t, state)
	if err := driver.Resize(1280, 1150); err != nil {
		t.Fatal(err)
	}
	input := hitOf(t, driver, hitUpdateAddress)
	typeAddress := func(draft string) {
		t.Helper()
		if err := driver.Click(input.Bounds.X+8, input.Bounds.Y+input.Bounds.Height/2); err != nil {
			t.Fatal(err)
		}
		if err := driver.SelectText(0, len(state.update.addressDraft)); err != nil {
			t.Fatal(err)
		}
		if err := driver.Type(draft); err != nil {
			t.Fatal(err)
		}
		clickPaintedLabel(t, driver, m.GeneralUpdateAddressSave)
	}
	typeAddress("owner/motty")
	waitUpdate(t, state, func() bool { return !state.update.checking })
	if state.update.address != "owner/motty" || state.update.addressInvalid {
		t.Fatalf("valid address not saved: %q", state.update.address)
	}
	typeAddress("http://invalid.example")
	if !state.update.addressInvalid || state.update.address != "owner/motty" {
		t.Fatal("an invalid draft replaced the saved repository")
	}
	reopened := newTestState(t, options)
	if reopened.update.source != update.SourceCustom || reopened.update.addressDraft != "owner/motty" || reopened.update.address != reopened.update.addressDraft {
		t.Fatalf("reopened source %v, address %q, draft %q", reopened.update.source, reopened.update.address, reopened.update.addressDraft)
	}
}

func TestSavedUpdateChoicesSurviveTheForkDefault(t *testing.T) {
	for _, tc := range []struct {
		source, address string
		wantSource      update.Source
		wantAddress     string
	}{
		{"custom", "", update.SourceCustom, update.DefaultCustomAddress},
		{"official", "", update.SourceOfficial, update.DefaultCustomAddress},
		{"cnb", "team/other", update.SourceCNB, "team/other"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			options := testOptions(t)
			saved := savedState{UpdateSource: tc.source, UpdateAddress: tc.address}
			if err := saveState(options.StatePath, saved); err != nil {
				t.Fatal(err)
			}
			state := newTestState(t, options)
			if state.update.source != tc.wantSource || state.update.address != tc.wantAddress || state.update.addressDraft != tc.wantAddress {
				t.Fatalf("restored source %v, address %q, draft %q", state.update.source, state.update.address, state.update.addressDraft)
			}
		})
	}
}
