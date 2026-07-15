package coverage

import (
	"fmt"

	"github.com/dukerupert/miranda/internal/domain"
)

// errOvernight is returned when a facility's close time is not after its open
// time. Overnight facilities are out of scope for v1 (spec §9); the engine
// returns a clear error rather than producing a negative-length day.
func errOvernight(f domain.Facility) error {
	return fmt.Errorf("facility %s: close %s not after open %s (overnight not supported)", f.ID, f.CloseTime, f.OpenTime)
}
