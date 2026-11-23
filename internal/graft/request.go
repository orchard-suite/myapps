package graft

import (
	"strings"
	"time"
)

// Request is one thing a person asked for, as the desk shows it to them.
type Request struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	State     string    `json:"state"`
	// Ended is true once the request has stopped.
	Ended bool `json:"ended"`
	// Repo is the repository it was sent against.
	Repo  string `json:"repo"`
	Title string `json:"title"`
	// Text is what the person sent; Answer the one message they get back.
	Text   string  `json:"text"`
	Answer string  `json:"answer"`
	Ticket *Ticket `json:"ticket"`
}

// Ticket is the ticket a request was filed as.
type Ticket struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
}

// The tones a status is drawn in.
const (
	ToneWorking = "working"
	ToneWaiting = "waiting"
	ToneDone    = "done"
	ToneStopped = "stopped"
)

// Status is a request's state in plain words, and the tone to draw it in.
type Status struct {
	Words string
	Tone  string
}

// statuses are the desk's states as a person reads them.
var statuses = map[string]Status{
	"received":        {"Received", ToneWorking},
	"screening":       {"Being checked", ToneWorking},
	"checking":        {"Being checked", ToneWorking},
	"investigating":   {"Being looked into", ToneWorking},
	"planning":        {"A fix is being planned", ToneWorking},
	"building":        {"A fix is being built", ToneWorking},
	"landing":         {"A fix is being released", ToneWorking},
	"watching":        {"Released, and being watched", ToneWorking},
	"holding":         {"Waiting for a known outage to end", ToneWaiting},
	"awaiting_plan":   {"Waiting for someone to approve the plan", ToneWaiting},
	"awaiting_review": {"Waiting for someone to review the fix", ToneWaiting},
	"answered":        {"Answered", ToneDone},
	"filed":           {"Passed to a person as a ticket", ToneDone},
	"waiting":         {"Waiting on a person", ToneWaiting},
	"released":        {"Fixed and released", ToneDone},
	"declined":        {"Declined", ToneStopped},
	"quarantined":     {"Set aside: it looked like it held a password or a key", ToneStopped},
	"failed":          {"Could not be finished", ToneStopped},
}

// Status is the request's state in plain words. A state this version does
// not know reads as being worked on, or closed once it has ended.
func (r Request) Status() Status {
	if s, ok := statuses[r.State]; ok {
		return s
	}
	if r.Ended {
		return Status{"Closed", ToneDone}
	}
	return Status{"Being worked on", ToneWorking}
}

// headingRunes is how much of a request's own words stand in for a title.
const headingRunes = 80

// Heading is the request's title, or the start of what the person wrote
// until the desk has given it one.
func (r Request) Heading() string {
	if t := strings.TrimSpace(r.Title); t != "" {
		return t
	}
	line, _, _ := strings.Cut(strings.TrimSpace(r.Text), "\n")
	line = strings.TrimSpace(line)
	if runes := []rune(line); len(runes) > headingRunes {
		return strings.TrimSpace(string(runes[:headingRunes])) + "..."
	}
	if line == "" {
		return "Request"
	}
	return line
}
