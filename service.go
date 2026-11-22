package checkin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"time"
)

type Service struct {
	config  Config
	backend Backend
	audit   Audit
	clock   func() time.Time
}

func New(config Config, backend Backend, audit Audit, clock func() time.Time) (*Service, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if backend == nil {
		return nil, errors.New("falta backend")
	}
	if audit == nil {
		audit = NewMemoryAudit()
	}
	if clock == nil {
		clock = time.Now
	}
	return &Service{config: config, backend: backend, audit: audit, clock: clock}, nil
}
func normalizedCode(code string) string { return strings.ToUpper(strings.TrimSpace(code)) }
func pointer(value string) *string      { return &value }
func (s *Service) result(input Input, ticket *Ticket, reason string, at *time.Time) Result {
	result := Result{Result: "allowed", Code: input.Code, CheckedAt: s.clock().UTC()}
	if reason != "" {
		result.Result = "rejected"
		result.Reason = pointer(reason)
	}
	if ticket != nil {
		result.HolderName = pointer(ticket.HolderName)
		result.SectorName = pointer(ticket.SectorName)
		result.TicketEventName = pointer(ticket.EventName)
	}
	if at != nil {
		copy := at.UTC()
		result.FirstEntryAt = &copy
	}
	return result
}
func validTicket(t Ticket, code string) bool {
	return validID(t.ID) && t.Code == code && validID(t.EventID) && strings.TrimSpace(t.EventName) != "" && strings.TrimSpace(t.HolderName) != "" && strings.TrimSpace(t.SectorName) != "" && (t.Status == "valid" || t.Status == "used" || t.Status == "cancelled")
}
func validContext(event EventContext, id string) bool {
	return event.EventID == id && validID(event.OrganizerID) && (event.Status == "draft" || event.Status == "published" || event.Status == "cancelled")
}
func classify(err error) *UpstreamError {
	var result *UpstreamError
	if errors.As(err, &result) {
		return result
	}
	return nil
}
func (s *Service) Scan(ctx context.Context, user User, input Input) (Result, *APIError) {
	if user.Role != "doorman" {
		return Result{}, forbidden()
	}
	input.Code = normalizedCode(input.Code)
	if !validID(input.EventID) || input.Code == "" || len(input.Code) > 128 {
		return Result{}, invalid()
	}
	event, err := s.backend.Context(ctx, input.EventID)
	if err != nil {
		if result := classify(err); result != nil && result.Status == 404 {
			return Result{}, missing()
		}
		return Result{}, unavailable()
	}
	if !validContext(event, input.EventID) {
		return Result{}, unavailable()
	}
	if event.Status != "published" {
		return s.result(input, nil, "invalid", nil), nil
	}
	ticket, err := s.backend.Ticket(ctx, input.Code)
	if err != nil {
		if result := classify(err); result != nil && result.Status == 404 {
			return s.result(input, nil, "invalid", nil), nil
		}
		return Result{}, unavailable()
	}
	if !validTicket(ticket, input.Code) {
		return Result{}, unavailable()
	}
	if ticket.Status == "cancelled" {
		return s.result(input, &ticket, "invalid", nil), nil
	}
	if ticket.EventID != input.EventID {
		return s.result(input, &ticket, "wrong_event", nil), nil
	}
	if ticket.Status == "used" {
		if ticket.UsedAt == nil || ticket.UsedAt.IsZero() {
			return Result{}, unavailable()
		}
		return s.result(input, &ticket, "already_used", ticket.UsedAt), nil
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return Result{}, unavailable()
	}
	use := UseInput{EventID: input.EventID, ScanID: "scan-" + hex.EncodeToString(random[:]), DoormanID: user.ID, DoormanName: user.Name}
	for attempt := 0; attempt < 2 && ctx.Err() == nil; attempt++ {
		receipt, err := s.backend.Use(ctx, input.Code, use)
		if err == nil {
			if !validTicket(receipt.Ticket, input.Code) || receipt.Ticket.ID != ticket.ID || receipt.Ticket.EventName != ticket.EventName || receipt.Ticket.HolderName != ticket.HolderName || receipt.Ticket.SectorName != ticket.SectorName || receipt.Ticket.EventID != input.EventID || receipt.Ticket.Status != "used" || receipt.Ticket.UsedAt == nil || !validUsage(receipt.Usage, input.EventID) || receipt.Usage.TicketID != ticket.ID || receipt.Usage.ScanID != use.ScanID || receipt.Usage.Code != input.Code || receipt.Usage.DoormanID != user.ID || receipt.Usage.DoormanName != user.Name || !receipt.Usage.UsedAt.Equal(*receipt.Ticket.UsedAt) || receipt.Usage.HolderName != receipt.Ticket.HolderName || receipt.Usage.SectorName != receipt.Ticket.SectorName {
				return Result{}, unavailable()
			}
			if s.audit.Record(receipt.Usage) != nil {
				return Result{}, unavailable()
			}
			return s.result(input, &receipt.Ticket, "", nil), nil
		}
		if e := classify(err); e != nil {
			if e.Status == 404 {
				return s.result(input, &ticket, "invalid", nil), nil
			}
			if e.Status == 409 {
				switch e.Code {
				case "invalid":
					return s.result(input, &ticket, "invalid", nil), nil
				case "wrong_event":
					return s.result(input, &ticket, "wrong_event", nil), nil
				case "already_used":
					if e.UsedAt != nil && !e.UsedAt.IsZero() {
						return s.result(input, &ticket, "already_used", e.UsedAt), nil
					}
				}
				return Result{}, unavailable()
			}
			if e.Status < 500 {
				return Result{}, unavailable()
			}
		}
	}
	// No background use: after an unknown result only read/reconcile confirmed usages.
	return Result{}, unavailable()
}
func (s *Service) Stats(ctx context.Context, user User, eventID string) (Stats, *APIError) {
	if user.Role != "doorman" && user.Role != "organizer" {
		return Stats{}, forbidden()
	}
	if !validID(eventID) {
		return Stats{}, invalid()
	}
	event, err := s.backend.Context(ctx, eventID)
	if err != nil {
		if e := classify(err); e != nil && e.Status == 404 {
			return Stats{}, missing()
		}
		return Stats{}, unavailable()
	}
	if !validContext(event, eventID) {
		return Stats{}, unavailable()
	}
	if (user.Role == "organizer" && event.OrganizerID != user.ID) || (user.Role == "doorman" && event.Status != "published") {
		return Stats{}, missing()
	}
	usages, err := s.backend.Usages(ctx, eventID)
	if err != nil {
		return Stats{}, unavailable()
	}
	if usages == nil {
		return Stats{}, unavailable()
	}
	// Read issued after usages. New entries may happen between calls; never invent them.
	counts, err := s.backend.Count(ctx, eventID)
	if err != nil {
		return Stats{}, unavailable()
	}
	if counts.EventID != eventID || counts.Issued < 0 || counts.Used < 0 || counts.Used > counts.Issued || counts.Used < len(usages) {
		return Stats{}, unavailable()
	}
	if s.audit.Import(eventID, usages) != nil {
		return Stats{}, unavailable()
	}
	usages = append([]Usage{}, usages...)
	sort.Slice(usages, func(i, j int) bool {
		if usages[i].UsedAt.Equal(usages[j].UsedAt) {
			return usages[i].Code < usages[j].Code
		}
		return usages[i].UsedAt.After(usages[j].UsedAt)
	})
	stats := Stats{EventID: eventID, Entered: len(usages), Issued: counts.Issued, Recent: []Record{}}
	for i, u := range usages {
		if i >= 10 {
			break
		}
		stats.Recent = append(stats.Recent, Record{Code: u.Code, HolderName: u.HolderName, SectorName: u.SectorName, At: u.UsedAt, DoormanName: u.DoormanName})
	}
	return stats, nil
}
