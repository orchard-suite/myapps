// Package graft sends a signed-in person's requests to a Graft request
// desk, and reads them back, over the desk's token port. My Apps' server
// says who the person is; a browser never does.
package graft

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// callTimeout bounds one call to the desk.
const callTimeout = 8 * time.Second

// maxAnswer bounds what is read of one answer from the desk.
const maxAnswer = 4 << 20

// The outcomes of a call, as OnCall hears them.
const (
	OutcomeOK      = "ok"
	OutcomeRefused = "refused"
	OutcomeFailed  = "failed"
)

// Report is what one person sends: their words about one application.
type Report struct {
	// Person is the signed-in person's address, from the front door.
	Person string
	Text   string
	// Repository is where the application's code lives, as the forge names
	// it; the desk needs it when this app reports against more than one.
	Repository string
	// URL is the application's address, and UserAgent the person's browser.
	URL       string
	UserAgent string
}

// Refusal is the desk saying no to the person, in words meant for them.
type Refusal struct {
	Status int
	Words  string
}

func (r *Refusal) Error() string { return fmt.Sprintf("graft refused (%d): %s", r.Status, r.Words) }

// Client calls one desk's token port as My Apps.
type Client struct {
	base      string
	tokenFile string
	http      *http.Client
	tracer    trace.Tracer
	onCall    func(ctx context.Context, operation, outcome string)
}

// New takes the token port's base, such as http://graft.example.com:8081,
// and the path of a file holding My Apps' intake token. The file is read on
// every call, so a rotated token works without a restart. onCall hears each
// call's operation (send or list) and outcome; nil hears nothing.
func New(base, tokenFile string, onCall func(ctx context.Context, operation, outcome string)) (*Client, error) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if u, err := url.Parse(base); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("GRAFT_URL must be an absolute URL, such as http://graft.example.com:8081")
	}
	if tokenFile == "" {
		return nil, errors.New("GRAFT_TOKEN_FILE must name a file")
	}
	if onCall == nil {
		onCall = func(context.Context, string, string) {}
	}
	return &Client{base: base, tokenFile: tokenFile, http: &http.Client{Timeout: callTimeout}, tracer: otel.Tracer("myapps/graft"), onCall: onCall}, nil
}

// Send hands the desk one person's report and answers the request it made.
func (c *Client) Send(ctx context.Context, r Report) (Request, error) {
	body := map[string]any{"person": r.Person, "text": r.Text}
	if r.Repository != "" {
		body["repository"] = r.Repository
	}
	about := map[string]string{}
	if r.URL != "" {
		about["url"] = r.URL
	}
	if r.UserAgent != "" {
		about["user_agent"] = r.UserAgent
	}
	if len(about) > 0 {
		body["context"] = about
	}
	var out Request
	err := c.call(ctx, "send", http.MethodPost, "/v1/requests", nil, body, &out)
	return out, err
}

// List answers what the person sent through My Apps, newest first.
func (c *Client) List(ctx context.Context, person string) ([]Request, error) {
	var out []Request
	err := c.call(ctx, "list", http.MethodGet, "/v1/requests", url.Values{"person": {person}}, nil, &out)
	return out, err
}

// call makes one call as its own client span. The span and every error
// name the operation and never the address called: the address carries the
// person. The trace continues into the desk.
func (c *Client) call(ctx context.Context, operation, method, path string, query url.Values, body, out any) (err error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	ctx, span := c.tracer.Start(ctx, "graft "+operation, trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(attribute.String("http.request.method", method), attribute.String("url.path", path)))
	outcome := OutcomeFailed
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, outcome)
		}
		span.SetAttributes(attribute.String("outcome", outcome))
		span.End()
		c.onCall(ctx, operation, outcome)
	}()

	raw, err := os.ReadFile(c.tokenFile)
	if err != nil {
		return fmt.Errorf("graft %s: reading the token file: %w", operation, err)
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		return fmt.Errorf("graft %s: the token file is empty", operation)
	}
	var send io.Reader
	if body != nil {
		enc, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("graft %s: %w", operation, err)
		}
		send = bytes.NewReader(enc)
	}
	target := c.base + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, target, send)
	if err != nil {
		return fmt.Errorf("graft %s: the request could not be made", operation)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("graft %s: %w", operation, withoutAddress(err))
	}
	defer res.Body.Close()
	span.SetAttributes(attribute.Int("http.response.status_code", res.StatusCode))
	answer, err := io.ReadAll(io.LimitReader(res.Body, maxAnswer))
	if err != nil {
		return fmt.Errorf("graft %s: reading the answer: %w", operation, withoutAddress(err))
	}
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		if err := json.Unmarshal(answer, out); err != nil {
			return fmt.Errorf("graft %s: the answer was not what the desk promises: %w", operation, err)
		}
		outcome = OutcomeOK
		return nil
	}
	var said struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(answer, &said)
	words := strings.TrimSpace(said.Error)
	// 400, 403, and 429 are about the person's words, their lane, or their
	// pace. Anything else, a refused token most of all, is My Apps' fault
	// and not the person's to read.
	switch res.StatusCode {
	case http.StatusBadRequest, http.StatusForbidden, http.StatusTooManyRequests:
		if words != "" {
			outcome = OutcomeRefused
			return &Refusal{Status: res.StatusCode, Words: words}
		}
	}
	return fmt.Errorf("graft %s: the desk answered %d", operation, res.StatusCode)
}

// withoutAddress is a transport error without the address it was calling.
func withoutAddress(err error) error {
	if u, ok := errors.AsType[*url.Error](err); ok {
		return u.Err
	}
	return err
}
