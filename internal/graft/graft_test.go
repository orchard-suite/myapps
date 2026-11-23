package graft

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// desk is a fake Graft token port and a client that calls it.
func desk(t *testing.T, h http.HandlerFunc) (*Client, string, *[]string) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	file := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(file, []byte("first-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var calls []string
	c, err := New(srv.URL+"/", file, func(_ context.Context, operation, outcome string) { calls = append(calls, operation+" "+outcome) })
	if err != nil {
		t.Fatal(err)
	}
	return c, file, &calls
}

func TestSendCarriesThePersonTheWordsAndWhereTheyWere(t *testing.T) {
	var got map[string]any
	var auth []string
	c, file, calls := desk(t, func(w http.ResponseWriter, r *http.Request) {
		auth = append(auth, r.Header.Get("Authorization"))
		if r.Method != http.MethodPost || r.URL.Path != "/v1/requests" {
			t.Errorf("sent %s %s", r.Method, r.URL.Path)
		}
		got = nil
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"r1","state":"received","text":"The page is blank."}`))
	})
	req, err := c.Send(context.Background(), Report{Person: "user@example.com", Text: "The page is blank.", Repository: "acme/portal", URL: "https://portal.example.com", UserAgent: "Browser/1"})
	if err != nil || req.ID != "r1" {
		t.Fatalf("send answered %+v, %v", req, err)
	}
	about, _ := got["context"].(map[string]any)
	if got["person"] != "user@example.com" || got["text"] != "The page is blank." || got["repository"] != "acme/portal" || about["url"] != "https://portal.example.com" || about["user_agent"] != "Browser/1" || len(got) != 4 || len(about) != 2 {
		t.Fatalf("the desk got %v", got)
	}
	// A rotated token is read on the next call.
	if err := os.WriteFile(file, []byte("second-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Send(context.Background(), Report{Person: "user@example.com", Text: "The page is blank."}); err != nil {
		t.Fatal(err)
	}
	if _, has := got["repository"]; has || got["context"] != nil {
		t.Fatalf("nothing unknown is sent: %v", got)
	}
	if strings.Join(auth, ",") != "Bearer first-token,Bearer second-token" {
		t.Fatalf("the token is read on each call, sent %v", auth)
	}
	if strings.Join(*calls, ",") != "send ok,send ok" {
		t.Fatalf("calls counted as %v", *calls)
	}
}

func TestTheDesksWordsForThePersonComeBackAsARefusal(t *testing.T) {
	status := http.StatusTooManyRequests
	c, _, calls := desk(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"error":"You have sent too many this hour."}`))
	})
	_, err := c.Send(context.Background(), Report{Person: "user@example.com", Text: "The page is blank."})
	refusal, ok := errors.AsType[*Refusal](err)
	if !ok || refusal.Status != http.StatusTooManyRequests || refusal.Words != "You have sent too many this hour." {
		t.Fatalf("a 429 is a refusal with the desk's words, got %v", err)
	}
	// A refused token is My Apps' fault: its words are not the person's.
	status = http.StatusUnauthorized
	_, err = c.List(context.Background(), "user@example.com")
	if _, ok := errors.AsType[*Refusal](err); ok || err == nil {
		t.Fatalf("a 401 is a failure, not a refusal: %v", err)
	}
	if strings.Join(*calls, ",") != "send refused,list failed" {
		t.Fatalf("calls counted as %v", *calls)
	}
}

func TestNoErrorNamesThePersonOrTheToken(t *testing.T) {
	c, file, _ := desk(t, func(http.ResponseWriter, *http.Request) {})
	c.base = "http://127.0.0.1:1"
	_, err := c.List(context.Background(), "user@example.com")
	if err == nil || strings.Contains(err.Error(), "example.com") || strings.Contains(err.Error(), "first-token") {
		t.Fatalf("an unreachable desk's error must not carry the address called: %v", err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if _, err := c.List(context.Background(), "user@example.com"); err == nil || strings.Contains(err.Error(), "user@") {
		t.Fatalf("a missing token file fails the call: %v", err)
	}
}

func TestListAsksForOnePerson(t *testing.T) {
	c, _, _ := desk(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/requests" || r.URL.Query().Get("person") != "user@example.com" {
			t.Errorf("asked %s %s", r.Method, r.URL)
		}
		_, _ = w.Write([]byte(`[{"id":"r2","state":"filed","ended":true,"title":"Blank page","ticket":{"number":7,"url":"https://git.example.com/acme/portal/issues/7"}},{"id":"r1","state":"investigating","text":"It is slow.\nSince Monday."}]`))
	})
	list, err := c.List(context.Background(), "user@example.com")
	if err != nil || len(list) != 2 {
		t.Fatalf("list answered %+v, %v", list, err)
	}
	if list[0].Ticket == nil || list[0].Ticket.Number != 7 || list[0].Status().Tone != ToneDone || list[0].Heading() != "Blank page" {
		t.Fatalf("the filed request reads %+v", list[0])
	}
	if list[1].Ticket != nil || list[1].Status().Words != "Being looked into" || list[1].Heading() != "It is slow." {
		t.Fatalf("the open request reads %+v %q", list[1], list[1].Heading())
	}
}

func TestEveryStateHasPlainWords(t *testing.T) {
	for _, state := range []string{"received", "screening", "checking", "investigating", "planning", "building", "landing", "watching", "holding", "awaiting_plan", "awaiting_review", "answered", "filed", "waiting", "released", "declined", "quarantined", "failed"} {
		s := Request{State: state}.Status()
		if s.Words == "" || s.Words == "Being worked on" || strings.Contains(s.Words, "_") || s.Tone == "" {
			t.Errorf("state %s has no words of its own: %+v", state, s)
		}
	}
	if s := (Request{State: "new_state", Ended: true}).Status(); s.Words != "Closed" {
		t.Fatalf("an unknown ended state is closed, got %+v", s)
	}
}

func TestNewNeedsAnAddressAndAFile(t *testing.T) {
	if _, err := New("graft:8081", "/run/token", nil); err == nil {
		t.Fatal("a bare host is not an address")
	}
	if _, err := New("http://graft.example.com:8081", "", nil); err == nil {
		t.Fatal("no token file is refused")
	}
}
