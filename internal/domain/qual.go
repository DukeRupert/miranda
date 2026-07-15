package domain

// Capability is a qualification tag a controller may hold. AP and LC are
// position qualifications; CIC is an orthogonal duty tag (see QualSet).
type Capability string

const (
	CapAP  Capability = "AP"  // Approach control
	CapLC  Capability = "LC"  // Local control
	CapCIC Capability = "CIC" // Controller-in-Charge (orthogonal duty tag)
)

// QualSet is a controller's set of held capabilities. A CPC ("Certified
// Professional Controller") holds {AP:true, LC:true}. CIC is orthogonal: any
// combination of position quals may or may not additionally include CIC, and an
// LC-only controller may legally hold CIC.
type QualSet map[Capability]bool

// Has reports whether the set holds capability c.
func (q QualSet) Has(c Capability) bool { return q[c] }

// HasAll reports whether the set holds every listed capability.
func (q QualSet) HasAll(cs ...Capability) bool {
	for _, c := range cs {
		if !q[c] {
			return false
		}
	}
	return true
}

// Superset reports whether q holds every capability that other holds (q ⊇
// other). Used for line-qualification checks: an occupant is eligible when
// their quals are a superset of the line's required quals.
func (q QualSet) Superset(other QualSet) bool {
	for c, need := range other {
		if need && !q[c] {
			return false
		}
	}
	return true
}

// Clone returns an independent copy so callers can mutate hypotheticals (e.g.
// downgrade candidates in LineQualRequirements) without touching the original.
func (q QualSet) Clone() QualSet {
	out := make(QualSet, len(q))
	for c, v := range q {
		if v {
			out[c] = true
		}
	}
	return out
}

// Without returns a copy of q with capability c removed. Used to model the
// "downgrade this occupant" hypotheticals in LineQualRequirements.
func (q QualSet) Without(c Capability) QualSet {
	out := q.Clone()
	delete(out, c)
	return out
}

// Convenience constructors for the common HLN qualification shapes.

// CPC is a controller qualified on all positions (AP + LC), optionally CIC.
func CPC(cic bool) QualSet {
	q := QualSet{CapAP: true, CapLC: true}
	if cic {
		q[CapCIC] = true
	}
	return q
}

// LCOnly is a controller qualified only on LC, optionally CIC.
func LCOnly(cic bool) QualSet {
	q := QualSet{CapLC: true}
	if cic {
		q[CapCIC] = true
	}
	return q
}
