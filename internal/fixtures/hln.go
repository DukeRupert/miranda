// Package fixtures holds the HLN (Helena) reference facility, rule set, shift
// templates, 9-line schedule, and controllers from spec §7–§8. It is a normal
// (non-test) package so the web UI can seed the same data the tests exercise.
package fixtures

import (
	"time"

	"github.com/dukerupert/miranda/internal/domain"
)

// HLNFacility is Helena: open 0545, close 2210, positions AP and LC.
func HLNFacility() domain.Facility {
	f, err := domain.NewFacility(
		"HLN", "Helena",
		domain.MustParseTimeOfDay("0545"),
		domain.MustParseTimeOfDay("2210"),
		[]domain.Position{
			{ID: "AP", Name: "Approach", Requires: domain.CapAP},
			{ID: "LC", Name: "Local", Requires: domain.CapLC},
		},
	)
	if err != nil {
		panic(err)
	}
	return f
}

// HLNRules is the HLN labor rule set (spec §7).
func HLNRules() domain.RuleSet {
	return domain.RuleSet{
		MaxShiftHours:       10 * time.Hour,
		MaxDaysPerWindow:    6,
		WindowDays:          7,
		RequiredHoursPerPP:  80 * time.Hour,
		MaxTimeOnPosition:   2 * time.Hour,
		MinBreak:            15 * time.Minute,
		MinStaffWhenOpen:    2,
		TurnaroundWarnHours: 10 * time.Hour,
		PositionSwapIsBreak: false,
	}
}

// Shift templates (spec §8). All are 8-hour shifts.
func tmpl(id, start string) domain.ShiftTemplate {
	s, err := domain.NewShiftTemplate(id, domain.MustParseTimeOfDay(start), 8*time.Hour)
	if err != nil {
		panic(err)
	}
	return s
}

// Templates returns the four HLN shift templates keyed by ID.
func Templates() map[string]domain.ShiftTemplate {
	return map[string]domain.ShiftTemplate{
		"E8":  tmpl("E8", "0545"),  // ends 1345
		"M8a": tmpl("M8a", "0745"), // ends 1545
		"M8b": tmpl("M8b", "1210"), // ends 2010
		"L8":  tmpl("L8", "1410"),  // ends 2210
	}
}

// HLNLines returns the 9-line reference schedule (spec §8). Index 0 is Sunday.
// A nil slot is an RDO. Each slot gets its own pointer to an immutable template.
func HLNLines() []domain.Line {
	t := Templates()
	// ref returns a fresh pointer to a copy of template id, or nil for RDO ("").
	ref := func(id string) *domain.ShiftTemplate {
		if id == "" {
			return nil
		}
		s := t[id]
		return &s
	}
	mk := func(id string, ids [7]string) domain.Line {
		var days [7]*domain.ShiftTemplate
		for i, s := range ids {
			days[i] = ref(s)
		}
		return domain.Line{ID: id, Days: days}
	}

	// Columns: Sun Mon Tue Wed Thu Fri Sat
	return []domain.Line{
		mk("E1", [7]string{"", "E8", "E8", "E8", "E8", "E8", ""}),
		mk("E2", [7]string{"E8", "M8a", "", "", "E8", "E8", "E8"}),
		mk("E3", [7]string{"E8", "E8", "E8", "E8", "", "", "E8"}),
		mk("M1", [7]string{"M8a", "M8a", "", "", "M8b", "M8a", "M8a"}),
		mk("M2", [7]string{"", "", "M8a", "M8a", "M8a", "M8a", "M8a"}),
		mk("M3", [7]string{"M8b", "M8b", "M8b", "M8b", "", "", "M8b"}),
		mk("L1", [7]string{"", "L8", "L8", "L8", "L8", "M8b", ""}),
		mk("L2", [7]string{"L8", "", "", "L8", "L8", "L8", "L8"}),
		mk("L3", [7]string{"L8", "L8", "L8", "", "", "L8", "L8"}),
	}
}

// HLNControllers returns nine controllers, one per line, all CPC holding CIC —
// the "all-CPC-with-CIC" body set from spec test (a). LineID is wired to the
// matching line.
func HLNControllers() []domain.Controller {
	lines := HLNLines()
	names := []string{"Adams", "Baker", "Clark", "Davis", "Evans", "Ford", "Gray", "Hill", "Irwin"}
	out := make([]domain.Controller, 0, len(lines))
	for i, line := range lines {
		id := line.ID
		out = append(out, domain.Controller{
			ID:     "C" + id,
			Name:   names[i],
			Quals:  domain.CPC(true),
			LineID: &id,
		})
	}
	return out
}

// OccupantQuals maps each line ID to its controller's quals (all CPC+CIC).
func OccupantQuals() map[string]domain.QualSet {
	m := map[string]domain.QualSet{}
	for _, c := range HLNControllers() {
		if c.LineID != nil {
			m[*c.LineID] = c.Quals
		}
	}
	return m
}
