package checkin

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

const testSecret = "test-only-jwt-secret-with-at-least-32-bytes"
const testKey = "test-only-internal-key-with-at-least-32-bytes"
const code = "EY-ABCD-EFGH"

func clock() time.Time { return time.Date(2030, 1, 1, 20, 0, 0, 0, time.UTC) }
func config() Config {
	return Config{Port: 4007, JWTSecret: testSecret, InternalAPIKey: testKey, TicketsURL: "http://localhost:4005", HTTPTimeout: time.Second, OperationTimeout: time.Second}
}
func porter() User {
	return User{ID: "u-door-1", Name: "Diego Mamani", Email: "portero@entradasya.bo", Role: "doorman"}
}
func originalTicket() Ticket {
	return Ticket{ID: "t-1", Code: code, EventID: "e-demo", EventName: "Evento Demo", HolderName: "Camila Rojas", SectorName: "General", Status: "valid"}
}
func copyTicket(ticket Ticket) Ticket {
	if ticket.UsedAt != nil {
		at := *ticket.UsedAt
		ticket.UsedAt = &at
	}
	return ticket
}

type fakeBackend struct {
	mu               sync.Mutex
	tickets          map[string]Ticket
	uses             map[string]Usage
	contexts         map[string]EventContext
	fail             bool
	failUse          int
	applyBeforeError bool
	useCalls         int
	scanIDs          []string
	delayUse         time.Duration
}

func fake() *fakeBackend {
	return &fakeBackend{tickets: map[string]Ticket{code: originalTicket()}, uses: map[string]Usage{}, contexts: map[string]EventContext{"e-demo": {EventID: "e-demo", OrganizerID: "u-org-1", Status: "published"}, "e-other": {EventID: "e-other", OrganizerID: "u-org-1", Status: "published"}}}
}
func (f *fakeBackend) Context(_ context.Context, id string) (EventContext, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return EventContext{}, errors.New("offline")
	}
	event, ok := f.contexts[id]
	if !ok {
		return EventContext{}, &UpstreamError{Status: 404}
	}
	return event, nil
}
func (f *fakeBackend) Ticket(_ context.Context, key string) (Ticket, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return Ticket{}, errors.New("offline")
	}
	ticket, ok := f.tickets[key]
	if !ok {
		return Ticket{}, &UpstreamError{Status: 404}
	}
	return copyTicket(ticket), nil
}
func (f *fakeBackend) Use(ctx context.Context, key string, input UseInput) (UseResult, error) {
	f.mu.Lock()
	delay := f.delayUse
	f.mu.Unlock()
	if delay > 0 {
		select {
		case <-ctx.Done():
			return UseResult{}, ctx.Err()
		case <-time.After(delay):
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.useCalls++
	f.scanIDs = append(f.scanIDs, input.ScanID)
	failing := f.failUse > 0
	if failing {
		f.failUse--
		if !f.applyBeforeError {
			return UseResult{}, errors.New("request not delivered")
		}
	}
	if usage, ok := f.uses[input.ScanID]; ok {
		if usage.Code != key || usage.EventID != input.EventID || usage.DoormanID != input.DoormanID || usage.DoormanName != input.DoormanName {
			return UseResult{}, &UpstreamError{Status: 409, Code: "idempotency_conflict"}
		}
		if failing {
			return UseResult{}, errors.New("response lost")
		}
		return UseResult{Ticket: copyTicket(f.tickets[key]), Usage: usage}, nil
	}
	ticket, ok := f.tickets[key]
	if !ok {
		return UseResult{}, &UpstreamError{Status: 404}
	}
	if ticket.Status == "cancelled" {
		return UseResult{}, &UpstreamError{Status: 409, Code: "invalid"}
	}
	if ticket.EventID != input.EventID {
		return UseResult{}, &UpstreamError{Status: 409, Code: "wrong_event"}
	}
	if ticket.Status == "used" {
		return UseResult{}, &UpstreamError{Status: 409, Code: "already_used", UsedAt: ticket.UsedAt}
	}
	at := clock().Add(time.Duration(len(f.uses)) * time.Minute)
	ticket.Status = "used"
	ticket.UsedAt = &at
	f.tickets[key] = ticket
	usage := Usage{ScanID: input.ScanID, TicketID: ticket.ID, Code: key, EventID: input.EventID, UsedAt: at, DoormanID: input.DoormanID, DoormanName: input.DoormanName, HolderName: ticket.HolderName, SectorName: ticket.SectorName}
	f.uses[input.ScanID] = usage
	if failing {
		return UseResult{}, errors.New("response lost")
	}
	return UseResult{Ticket: copyTicket(ticket), Usage: usage}, nil
}
func (f *fakeBackend) Usages(_ context.Context, id string) ([]Usage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return nil, errors.New("offline")
	}
	usages := []Usage{}
	for _, usage := range f.uses {
		if usage.EventID == id {
			usages = append(usages, usage)
		}
	}
	return usages, nil
}
func (f *fakeBackend) Count(_ context.Context, id string) (Counts, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return Counts{}, errors.New("offline")
	}
	counts := Counts{EventID: id}
	for _, ticket := range f.tickets {
		if ticket.EventID == id && ticket.Status != "cancelled" {
			counts.Issued++
			if ticket.Status == "used" {
				counts.Used++
			}
		}
	}
	return counts, nil
}
func service(t *testing.T, backend Backend, audit Audit) *Service {
	t.Helper()
	s, err := New(config(), backend, audit, clock)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func scan(t *testing.T, s *Service, key string) Result {
	t.Helper()
	result, err := s.Scan(context.Background(), porter(), Input{Code: key, EventID: "e-demo"})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func TestFirstEntryAndSecondScanRejected(t *testing.T) {
	f := fake()
	audit := NewMemoryAudit()
	s := service(t, f, audit)
	first := scan(t, s, "  ey-abcd-efgh  ")
	if first.Result != "allowed" || first.Reason != nil || first.Code != code || first.FirstEntryAt != nil || *first.HolderName != "Camila Rojas" {
		t.Fatal(first)
	}
	second := scan(t, s, code)
	if second.Result != "rejected" || *second.Reason != "already_used" || second.FirstEntryAt == nil || !second.FirstEntryAt.Equal(clock()) {
		t.Fatal(second)
	}
	if len(audit.entries) != 1 || len(f.uses) != 1 {
		t.Fatal("duplicate entry")
	}
}
func TestWrongEventCancelledAndInvalidCode(t *testing.T) {
	for _, kind := range []string{"wrong_event", "invalid", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			f := fake()
			key := code
			if kind == "wrong_event" {
				ticket := f.tickets[code]
				ticket.EventID = "e-other"
				f.tickets[code] = ticket
			}
			if kind == "cancelled" {
				ticket := f.tickets[code]
				ticket.Status = "cancelled"
				f.tickets[code] = ticket
			}
			if kind == "invalid" {
				key = "EY-XXXX-0000"
			}
			result := scan(t, service(t, f, nil), key)
			want := kind
			if kind == "cancelled" {
				want = "invalid"
			}
			if result.Result != "rejected" || *result.Reason != want || len(f.uses) != 0 {
				t.Fatal(result)
			}
			if kind == "invalid" && (result.HolderName != nil || result.SectorName != nil || result.TicketEventName != nil) {
				t.Fatal("unknown ticket leaked metadata")
			}
		})
	}
}
func TestLostResponseRetriesSameScanWithoutDoubleUse(t *testing.T) {
	f := fake()
	f.failUse = 1
	f.applyBeforeError = true
	s := service(t, f, nil)
	result := scan(t, s, code)
	if result.Result != "allowed" || f.useCalls != 2 || len(f.uses) != 1 || f.scanIDs[0] != f.scanIDs[1] {
		t.Fatal(result, f.scanIDs)
	}
}
func TestUnknownResultReconcilesOnlyConfirmedUsage(t *testing.T) {
	f := fake()
	f.failUse = 2
	f.applyBeforeError = true
	audit := NewMemoryAudit()
	s := service(t, f, audit)
	_, err := s.Scan(context.Background(), porter(), Input{Code: code, EventID: "e-demo"})
	if err == nil || err.Status != 503 || len(audit.entries) != 0 || len(f.uses) != 1 {
		t.Fatal(err)
	}
	stats, err := s.Stats(context.Background(), porter(), "e-demo")
	if err != nil || stats.Entered != 1 || stats.Issued != 1 || len(audit.entries) != 1 {
		t.Fatal(stats, err)
	}
	result := scan(t, s, code)
	if *result.Reason != "already_used" || f.useCalls != 2 {
		t.Fatal(result)
	}
}
func TestUndeliveredAttemptsDoNotCreateBackgroundAdmissions(t *testing.T) {
	f := fake()
	f.failUse = 2
	s := service(t, f, nil)
	_, err := s.Scan(context.Background(), porter(), Input{Code: code, EventID: "e-demo"})
	if err == nil || err.Status != 503 {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	stats, err := s.Stats(context.Background(), porter(), "e-demo")
	if err != nil || stats.Entered != 0 || f.useCalls != 2 || f.tickets[code].Status != "valid" {
		t.Fatal(stats, err)
	}
}

type failAudit struct {
	*MemoryAudit
	fail bool
}

func (a *failAudit) Record(u Usage) error {
	if a.fail {
		a.fail = false
		return errors.New("audit write failed")
	}
	return a.MemoryAudit.Record(u)
}
func TestAuditFailureAfterConsumptionRecoveredByStats(t *testing.T) {
	f := fake()
	audit := &failAudit{MemoryAudit: NewMemoryAudit(), fail: true}
	s := service(t, f, audit)
	_, err := s.Scan(context.Background(), porter(), Input{Code: code, EventID: "e-demo"})
	if err == nil || err.Status != 503 {
		t.Fatal(err)
	}
	stats, err := s.Stats(context.Background(), porter(), "e-demo")
	if err != nil || stats.Entered != 1 || len(audit.entries) != 1 {
		t.Fatal(stats, err)
	}
	if *scan(t, s, code).Reason != "already_used" {
		t.Fatal("entry unlocked")
	}
}
func TestConcurrentPortersOnlyOneAdmission(t *testing.T) {
	f := fake()
	audit := NewMemoryAudit()
	s := service(t, f, audit)
	var wg sync.WaitGroup
	results := make(chan Result, 40)
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := s.Scan(context.Background(), porter(), Input{Code: code, EventID: "e-demo"})
			if err != nil {
				t.Error(err)
				return
			}
			results <- result
		}()
	}
	wg.Wait()
	close(results)
	allowed := 0
	for result := range results {
		if result.Result == "allowed" {
			allowed++
		} else if result.Reason == nil || *result.Reason != "already_used" {
			t.Fatal(result)
		}
	}
	if allowed != 1 || len(f.uses) != 1 || len(audit.entries) != 1 {
		t.Fatal("duplicate admissions", allowed)
	}
}
func TestStatsOwnershipAndPublishedPorterScope(t *testing.T) {
	f := fake()
	s := service(t, f, nil)
	for _, user := range []User{{ID: "u-cli-1", Role: "customer"}, {ID: "u-org-2", Role: "organizer"}} {
		_, err := s.Stats(context.Background(), user, "e-demo")
		if err == nil || (user.Role == "customer" && err.Status != 403) || (user.Role == "organizer" && err.Status != 404) {
			t.Fatal(err)
		}
	}
	for _, role := range []string{"customer", "organizer"} {
		_, err := s.Scan(context.Background(), User{ID: "u-org-1", Role: role}, Input{Code: code, EventID: "e-demo"})
		if err == nil || err.Status != 403 {
			t.Fatal(err)
		}
	}
	event := f.contexts["e-demo"]
	event.Status = "cancelled"
	f.contexts["e-demo"] = event
	if _, err := s.Stats(context.Background(), porter(), "e-demo"); err == nil || err.Status != 404 {
		t.Fatal(err)
	}
	stats, err := s.Stats(context.Background(), User{ID: "u-org-1", Role: "organizer"}, "e-demo")
	if err != nil || stats.Entered != 0 || stats.Recent == nil {
		t.Fatal(stats, err)
	}
}
func TestStatsHasTenMostRecentAndNeverCountsRejections(t *testing.T) {
	f := fake()
	f.tickets = map[string]Ticket{}
	for i := 0; i < 15; i++ {
		ticket := originalTicket()
		ticket.ID = fmt.Sprintf("t-%d", i)
		ticket.Code = fmt.Sprintf("EY-DEMO-%04d", i)
		f.tickets[ticket.Code] = ticket
	}
	s := service(t, f, nil)
	for i := 0; i < 15; i++ {
		scan(t, s, fmt.Sprintf("EY-DEMO-%04d", i))
	}
	scan(t, s, "EY-UNKNOWN-AAAA")
	stats, err := s.Stats(context.Background(), porter(), "e-demo")
	if err != nil || stats.Entered != 15 || stats.Issued != 15 || len(stats.Recent) != 10 || stats.Recent[0].Code != "EY-DEMO-0014" {
		t.Fatal(stats, err)
	}
}
func TestDependencyFailureDoesNotGrantOrMislabelEntry(t *testing.T) {
	f := fake()
	f.fail = true
	s := service(t, f, nil)
	if _, err := s.Scan(context.Background(), porter(), Input{Code: code, EventID: "e-demo"}); err == nil || err.Status != 503 {
		t.Fatal(err)
	}
	if len(f.uses) != 0 {
		t.Fatal("consumed while dependency offline")
	}
}
func TestOperationDeadline(t *testing.T) {
	f := fake()
	f.delayUse = time.Second
	c := config()
	c.OperationTimeout = 20 * time.Millisecond
	s, err := New(c, f, nil, clock)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.OperationTimeout)
	defer cancel()
	start := time.Now()
	if _, err := s.Scan(ctx, porter(), Input{Code: code, EventID: "e-demo"}); err == nil || err.Status != 503 {
		t.Fatal(err)
	}
	if time.Since(start) > 500*time.Millisecond || len(f.uses) != 0 {
		t.Fatal("deadline or consumption failure")
	}
}
func TestAuditImportIsAtomicAndIdempotent(t *testing.T) {
	a := NewMemoryAudit()
	u := Usage{ScanID: "scan-1", TicketID: "t-1", Code: code, EventID: "e-demo", UsedAt: clock(), DoormanID: "u-door-1", DoormanName: "Portero", HolderName: "Camila", SectorName: "General"}
	if a.Record(u) != nil || a.Record(u) != nil || len(a.entries) != 1 {
		t.Fatal("not idempotent")
	}
	other := u
	other.ScanID = "scan-2"
	other.TicketID = "t-2"
	bad := u
	bad.TicketID = "t-3"
	if a.Import("e-demo", []Usage{other, bad}) == nil || len(a.entries) != 1 {
		t.Fatal("partial invalid import")
	}
}
func TestConfigValidation(t *testing.T) {
	for _, edit := range []func(*Config){func(c *Config) { c.Port = 0 }, func(c *Config) { c.JWTSecret = "short" }, func(c *Config) { c.TicketsURL = "http://user:secret@localhost" }, func(c *Config) { c.TicketsURL = "http://localhost?" }, func(c *Config) { c.OperationTimeout = 7 * time.Second }} {
		c := config()
		edit(&c)
		if c.Validate() == nil {
			t.Fatal("accepted invalid config")
		}
	}
}

type corruptReceipt struct{ *fakeBackend }

func (f corruptReceipt) Use(ctx context.Context, key string, input UseInput) (UseResult, error) {
	result, err := f.fakeBackend.Use(ctx, key, input)
	if err == nil {
		result.Usage.TicketID = "wrong-ticket"
	}
	return result, err
}
func TestMalformedConsumptionReceiptNeverGrantsAdmission(t *testing.T) {
	f := fake()
	s := service(t, corruptReceipt{f}, nil)
	if _, err := s.Scan(context.Background(), porter(), Input{Code: code, EventID: "e-demo"}); err == nil || err.Status != 503 {
		t.Fatal(err)
	}
	stats, err := s.Stats(context.Background(), porter(), "e-demo")
	if err != nil || stats.Entered != 1 {
		t.Fatal(stats, err)
	}
}
