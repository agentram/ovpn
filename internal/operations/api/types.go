// Package api defines credential-free management protocol projections.
package api

import (
	"encoding/json"
	"time"
)

// Field describes a structured command parameter. Secret defaults are never exposed.
type Field struct {
	Options  []string `json:"options,omitempty"`
	Name     string   `json:"name"`
	Label    string   `json:"label"`
	Type     string   `json:"type"`
	Default  string   `json:"default,omitempty"`
	Required bool     `json:"required"`
	Secret   bool     `json:"secret"`
	Advanced bool     `json:"advanced"`
}

type Action struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Area        string  `json:"area"`
	Description string  `json:"description"`
	Mutation    bool    `json:"mutation"`
	Destructive bool    `json:"destructive"`
	Fields      []Field `json:"fields"`
	Arguments   []Field `json:"arguments"`
}

type Request struct {
	Action    string            `json:"action"`
	Values    map[string]string `json:"values"`
	Arguments []string          `json:"arguments"`
}

type Target struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Plan struct {
	StopOnFailure bool      `json:"stopOnFailure,omitempty"`
	ID            string    `json:"id"`
	Action        string    `json:"action"`
	Title         string    `json:"title"`
	Digest        string    `json:"digest"`
	Targets       []Target  `json:"targets"`
	Destructive   bool      `json:"destructive"`
	Mutation      bool      `json:"mutation"`
	CreatedAt     time.Time `json:"createdAt"`
	// Request and snapshots stay in the private journal, outside wire projections.
	Request  Request         `json:"-"`
	Revision string          `json:"-"`
	Snapshot json.RawMessage `json:"-"`
}

type Start struct {
	PlanID    string `json:"planId"`
	Digest    string `json:"digest"`
	Key       string `json:"key"`
	Confirmed bool   `json:"confirmed"`
}

type Step struct {
	Target Target `json:"target"`
	State  string `json:"state"`
	Code   string `json:"code,omitempty"`
}

type Job struct {
	ID         string     `json:"id"`
	PlanID     string     `json:"planId"`
	Action     string     `json:"action"`
	Title      string     `json:"title"`
	State      string     `json:"state"`
	Code       string     `json:"code,omitempty"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	Steps      []Step     `json:"steps"`
}

type Result struct {
	Data      json.RawMessage `json:"data,omitempty"`
	QRData    string          `json:"qrData,omitempty"`
	Text      string          `json:"text,omitempty"`
	Content   string          `json:"content,omitempty"`
	MediaType string          `json:"mediaType,omitempty"`
}
