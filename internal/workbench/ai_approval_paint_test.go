package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"motty/internal/ai/agent"
	"motty/internal/ai/gate"
	"motty/internal/ai/risk"
	"motty/internal/i18n"
)

// The pinned question is a card, and the card's own background is one of the
// page's children. The confirming buttons are slots painted by hand, so they
// must be painted after those children; a review found the Allow button reduced
// to its label, near-black on the dark card, because the card's fill covered
// every button surface. This asserts the invariant that matters: whatever is
// painted last over the middle of the button is the button's own surface, not
// something behind it.
func TestTheApprovalButtonsSurfaceIsPaintedOverTheCard(t *testing.T) {
	state, _ := panelState(t, nil)
	withModel(t, state)
	state.ai.mode = agent.ModeAgent
	state.setAIPanelOpen(true)
	state.ai.approvals = append(state.ai.approvals, &aiApproval{request: gate.Request{
		Tool:         "terminal_exec",
		Summary:      "touch created-by-assistant",
		SessionLabel: "staging-web",
		Assessment:   risk.Assessment{Level: risk.Medium},
	}})

	canvas, _ := paintAt(state, panelWide)
	approve := panelHit(t, canvas, hitAIApprove)
	want := components.ButtonRecipe(state.Theme(), components.ButtonDefault, gfx.State{}).Background
	center := gfx.Point{
		X: approve.Bounds.X + approve.Bounds.Width/2,
		Y: approve.Bounds.Y + approve.Bounds.Height/2,
	}

	var last gfx.Rectangle
	found := false
	for _, rectangle := range canvas.Rectangles() {
		bounds := rectangle.Bounds
		if center.X < bounds.X || center.Y < bounds.Y ||
			center.X >= bounds.X+bounds.Width || center.Y >= bounds.Y+bounds.Height {
			continue
		}
		// A translucent tint lets the layers under it show; only an opaque fill
		// can hide the button's surface.
		if rectangle.Color.Alpha < 1 {
			continue
		}
		last, found = rectangle, true
	}
	if !found || last.Bounds != approve.Bounds || last.Color != want {
		t.Fatalf("what is painted last over the approval button's middle is %+v, want the button's own fill %+v at %+v",
			last, want, approve.Bounds)
	}
}

func TestApprovalRiskBadgeShowsTheCurrentReasonsOnHover(t *testing.T) {
	state := agentPanel(t)
	state.setLanguage(i18n.Chinese)
	state.ai.panelWidth = aiPanelMin
	const command = `sudo -n ss -H -tulnp | python3 -c 'import sys; print(len(sys.stdin.readlines()))'`
	assessment := risk.Classify(command)
	if assessment.Level != risk.High || len(assessment.Reasons) < 2 {
		t.Fatalf("unexpected assessment for the piped command: %+v", assessment)
	}
	first := approvalFixture(state, risk.Critical, true)
	first.request.Assessment.Reasons = []string{"", "  affects all volumes  "}
	second := approvalFixture(state, risk.High, false)
	second.request.Summary, second.request.Assessment = command, assessment
	view := openAIPanelView(t, state)
	defer view.Close()
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	checkHover := func(label string, reasons []string) gfx.Rect {
		t.Helper()
		badge, ok := canvasHit(view.Snapshot().Hits, hitAIApprovalRisk)
		if !ok || badge.Label != label || badge.Hint != strings.Join(reasons, "\n") {
			t.Fatalf("risk badge = %+v, want %q with reasons %q", badge, label, reasons)
		}
		b := badge.Bounds
		if err := view.Move(b.Right()-12, b.Y+b.Height/2); err != nil {
			t.Fatal(err)
		}
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
		var lines []string
		for _, text := range view.Snapshot().Texts {
			if text.Layer == tooltipLayer {
				lines = append(lines, text.Value)
			}
		}
		if got := strings.Join(lines, " "); got != strings.Join(reasons, " ") {
			t.Fatalf("hovered tooltip = %q, want reasons %q", got, reasons)
		}
		return b
	}
	checkHover(state.messages.AIPanel.RiskCritical, []string{"affects all volumes"})
	state.dropApproval(first)
	if err := view.Driver().Paint(); err != nil {
		t.Fatal(err)
	}
	b := checkHover(state.messages.AIPanel.RiskHigh, assessment.Reasons)
	if err := view.Move(b.X-4, b.Y+b.Height/2); err != nil {
		t.Fatal(err)
	}
	if state.tooltip.Showing() == hitAIApprovalRisk {
		t.Fatal("risk reasons stayed visible after the pointer left the badge")
	}
	if err := view.Click(b.Right()-12, b.Y+b.Height/2); err != nil {
		t.Fatal(err)
	}
	if len(state.ai.approvals) != 1 || state.ai.approvals[0] != second {
		t.Fatal("clicking the risk badge answered the pending approval")
	}
}

func TestApprovalRiskBadgeOmitsHelpWithoutHighRiskReasons(t *testing.T) {
	state := agentPanel(t)
	for _, tc := range []struct {
		level      risk.Level
		reasons    []string
		escalation bool
	}{
		{risk.High, nil, false},
		{risk.Critical, []string{" ", "\t"}, false},
		{risk.Medium, []string{"changes a file"}, false},
		{risk.High, []string{"reason for switching modes"}, true},
	} {
		approval := approvalFixture(state, tc.level, false)
		approval.request.Assessment.Reasons, approval.escalation = tc.reasons, tc.escalation
		canvas, _ := paintAt(state, panelWide)
		if hasHit(canvas, hitAIApprovalRisk) {
			t.Errorf("unexpected risk help for %+v", tc)
		}
		state.dropApproval(approval)
	}
}
