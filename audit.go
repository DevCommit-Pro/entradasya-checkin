package checkin

import (
	"errors"
	"strings"
	"sync"
)

type Audit interface {
	Record(Usage) error
	Import(string, []Usage) error
}
type MemoryAudit struct {
	mu      sync.Mutex
	entries map[string]Usage
	scans   map[string]string
}

func NewMemoryAudit() *MemoryAudit {
	return &MemoryAudit{entries: map[string]Usage{}, scans: map[string]string{}}
}
func validID(value string) bool { return strings.TrimSpace(value) != "" && len(value) <= 128 }
func validUsage(u Usage, event string) bool {
	return validID(event) && validID(u.ScanID) && validID(u.TicketID) && u.EventID == event && validID(u.DoormanID) && u.Code != "" && u.Code == normalizedCode(u.Code) && !u.UsedAt.IsZero() && strings.TrimSpace(u.DoormanName) != "" && strings.TrimSpace(u.HolderName) != "" && strings.TrimSpace(u.SectorName) != ""
}
func sameUsage(a, b Usage) bool {
	return a.ScanID == b.ScanID && a.TicketID == b.TicketID && a.EventID == b.EventID && a.Code == b.Code && a.UsedAt.Equal(b.UsedAt) && a.DoormanID == b.DoormanID && a.DoormanName == b.DoormanName && a.HolderName == b.HolderName && a.SectorName == b.SectorName
}
func (a *MemoryAudit) Record(u Usage) error { return a.Import(u.EventID, []Usage{u}) }
func (a *MemoryAudit) Import(event string, usages []Usage) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	ids := map[string]bool{}
	scans := map[string]bool{}
	for _, u := range usages {
		if !validUsage(u, event) || ids[u.TicketID] || scans[u.ScanID] {
			return errors.New("invalid audit snapshot")
		}
		ids[u.TicketID] = true
		scans[u.ScanID] = true
		if previous, ok := a.entries[u.TicketID]; ok && !sameUsage(previous, u) {
			return errors.New("conflicting ticket audit")
		}
		if previous, ok := a.scans[u.ScanID]; ok && previous != u.TicketID {
			return errors.New("conflicting scan audit")
		}
	}
	for _, u := range usages {
		a.entries[u.TicketID] = u
		a.scans[u.ScanID] = u.TicketID
	}
	return nil
}
