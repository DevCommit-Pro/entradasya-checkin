package checkin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/golang-jwt/jwt/v5"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func token(t *testing.T, id, role string) string {
	t.Helper()
	value, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": id, "name": "Diego Mamani", "email": "portero@entradasya.bo", "role": role, "iat": time.Now().Add(-time.Minute).Unix(), "exp": time.Now().Add(time.Hour).Unix()}).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatal(err)
	}
	return value
}
func call(t *testing.T, s *Service, method, path, jwt, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if jwt != "" {
		req.Header.Set("Authorization", "Bearer "+jwt)
	}
	out := httptest.NewRecorder()
	s.Handler(nil).ServeHTTP(out, req)
	return out
}
func decode(t *testing.T, out *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if json.Unmarshal(out.Body.Bytes(), &body) != nil {
		t.Fatal(out.Body.String())
	}
	return body
}
func payload() string { return `{"code":"EY-ABCD-EFGH","eventId":"e-demo"}` }
func TestHTTPAllowedAndBusinessRejectionStay200(t *testing.T) {
	s := service(t, fake(), nil)
	jwt := token(t, "u-door-1", "doorman")
	first := call(t, s, "POST", "/checkin", jwt, payload())
	body := decode(t, first)
	if first.Code != 200 || body["result"] != "allowed" || body["reason"] != nil || body["firstEntryAt"] != nil || body["holderName"] != "Camila Rojas" {
		t.Fatal(first.Code, body)
	}
	second := call(t, s, "POST", "/checkin", jwt, payload())
	body = decode(t, second)
	if second.Code != 200 || body["result"] != "rejected" || body["reason"] != "already_used" || body["firstEntryAt"] == nil {
		t.Fatal(second.Code, body)
	}
	if first.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("private response cacheable")
	}
}
func TestHTTPNullFieldsForUnknownCode(t *testing.T) {
	s := service(t, fake(), nil)
	response := call(t, s, "POST", "/checkin", token(t, "u-door-1", "doorman"), `{"code":"EY-XXXX-0000","eventId":"e-demo"}`)
	body := decode(t, response)
	if response.Code != 200 || body["reason"] != "invalid" {
		t.Fatal(response.Code, body)
	}
	for _, field := range []string{"holderName", "sectorName", "ticketEventName", "firstEntryAt"} {
		value, present := body[field]
		if !present || value != nil {
			t.Fatal("field must be present/null", field)
		}
	}
}
func TestHTTPAuthRolesValidationAndSpoofedMetadata(t *testing.T) {
	s := service(t, fake(), nil)
	jwt := token(t, "u-door-1", "doorman")
	for _, item := range []struct {
		jwt, body string
		status    int
	}{{"", payload(), 401}, {token(t, "u-cli-1", "customer"), payload(), 403}, {token(t, "u-org-1", "organizer"), payload(), 403}, {jwt, "{", 400}, {jwt, `{"eventId":"e-demo","code":""}`, 400}, {jwt, `{"eventId":"e-demo","code":"EY-ABCD-EFGH","scanId":"fake"}`, 400}, {jwt, `{"eventId":"e-demo","code":"EY-ABCD-EFGH","doormanId":"u-org-1"}`, 400}} {
		response := call(t, s, "POST", "/checkin", item.jwt, item.body)
		if response.Code != item.status || decode(t, response)["error"] == nil {
			t.Fatal(response.Code, response.Body.String())
		}
	}
}
func TestHTTPStatsForOwnerAndPrivateDataProtection(t *testing.T) {
	s := service(t, fake(), nil)
	call(t, s, "POST", "/checkin", token(t, "u-door-1", "doorman"), payload())
	response := call(t, s, "GET", "/checkin/events/e-demo/stats", token(t, "u-org-1", "organizer"), "")
	body := decode(t, response)
	if response.Code != 200 || body["entered"] != float64(1) || body["issued"] != float64(1) || len(body["recent"].([]any)) != 1 {
		t.Fatal(response.Code, body)
	}
	for _, item := range []struct {
		jwt    string
		status int
	}{{"", 401}, {token(t, "u-org-2", "organizer"), 404}, {token(t, "u-cli-1", "customer"), 403}} {
		response = call(t, s, "GET", "/checkin/events/e-demo/stats", item.jwt, "")
		if response.Code != item.status {
			t.Fatal(response.Code, response.Body.String())
		}
	}
}
func TestHTTPDependencyUnknownIs503NotRejected(t *testing.T) {
	f := fake()
	f.failUse = 2
	s := service(t, f, nil)
	response := call(t, s, "POST", "/checkin", token(t, "u-door-1", "doorman"), payload())
	body := decode(t, response)
	if response.Code != 503 || body["result"] != nil || body["error"].(map[string]any)["code"] != "service_unavailable" {
		t.Fatal(response.Code, body)
	}
}
func TestHTTPInvalidTokenHealthMethodsAndUnknownRoutes(t *testing.T) {
	s := service(t, fake(), nil)
	if response := call(t, s, "POST", "/checkin", "bad.jwt.signature", payload()); response.Code != 401 {
		t.Fatal(response.Code)
	}
	if response := call(t, s, "GET", "/health", "", ""); response.Code != 200 || decode(t, response)["storage"] != "memory" {
		t.Fatal(response.Code)
	}
	if response := call(t, s, "DELETE", "/checkin", "", ""); response.Code != 405 || response.Header().Get("Allow") != "POST" {
		t.Fatal(response.Code)
	}
	if response := call(t, s, "GET", "/unknown", "", ""); response.Code != 404 {
		t.Fatal(response.Code)
	}
}
func TestNoCodesNamesOrSecretsInLogs(t *testing.T) {
	s := service(t, fake(), nil)
	jwt := token(t, "u-door-1", "doorman")
	req := httptest.NewRequest("POST", "/checkin", strings.NewReader(payload()))
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Content-Type", "application/json")
	var logs bytes.Buffer
	s.Handler(slog.New(slog.NewJSONHandler(&logs, nil))).ServeHTTP(httptest.NewRecorder(), req)
	for _, sensitive := range []string{jwt, testKey, code, "Camila Rojas", "Diego Mamani"} {
		if strings.Contains(logs.String(), sensitive) {
			t.Fatal("sensitive log")
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestProductionHTTPClientWireContractsAndSameScanRetry(t *testing.T) {
	c := config()
	backend := NewHTTPBackend(c)
	var saved UseInput
	ticket := originalTicket()
	useCalls := 0
	backend.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("X-Internal-Key") != testKey || r.Header.Get("Authorization") != "" {
			return nil, errors.New("invalid internal authentication")
		}
		status := 200
		var result any
		switch {
		case strings.HasSuffix(r.URL.Path, "/context"):
			result = EventContext{EventID: "e-demo", OrganizerID: "u-org-1", Status: "published"}
		case r.URL.Path == "/internal/tickets/by-code/"+code:
			result = ticket
		case strings.HasSuffix(r.URL.Path, "/use"):
			var in UseInput
			if json.NewDecoder(r.Body).Decode(&in) != nil || in.DoormanID != "u-door-1" || in.DoormanName != "Diego Mamani" || in.ScanID == "" {
				return nil, errors.New("bad use payload")
			}
			useCalls++
			if useCalls == 1 {
				saved = in
				at := clock()
				ticket.Status = "used"
				ticket.UsedAt = &at
				status = 503
				result = map[string]any{"error": map[string]string{"code": "service_unavailable"}}
			} else {
				if in != saved {
					return nil, errors.New("scan changed during retry")
				}
				result = UseResult{Ticket: ticket, Usage: Usage{ScanID: in.ScanID, TicketID: ticket.ID, Code: code, EventID: in.EventID, UsedAt: clock(), DoormanID: in.DoormanID, DoormanName: in.DoormanName, HolderName: ticket.HolderName, SectorName: ticket.SectorName}}
			}
		default:
			return nil, errors.New("unexpected route")
		}
		raw, err := json.Marshal(result)
		if err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(raw))}, nil
	})
	s := service(t, backend, nil)
	result, err := s.Scan(context.Background(), porter(), Input{Code: code, EventID: "e-demo"})
	if err != nil || result.Result != "allowed" || useCalls != 2 {
		t.Fatal(result, err, useCalls)
	}
}
func TestUpstreamAlreadyUsedPreservesOriginalTimestamp(t *testing.T) {
	backend := NewHTTPBackend(config())
	backend.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 409, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":{"code":"already_used","message":"Usada"},"usedAt":"2030-01-01T20:00:00Z"}`))}, nil
	})
	_, err := backend.Use(context.Background(), code, UseInput{})
	upstream := classify(err)
	if upstream == nil || upstream.Code != "already_used" || upstream.UsedAt == nil || !upstream.UsedAt.Equal(clock()) {
		t.Fatal(err)
	}
}
