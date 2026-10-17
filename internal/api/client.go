// Package api is a small client for Ampworth's public JSON API. It holds no
// calculations of its own: every answer comes from the server, so the CLI and
// the website can never disagree.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultBaseURL is the API the CLI talks to unless told otherwise.
const DefaultBaseURL = "https://ampworth.com/api/v1"

// Client calls the API.
type Client struct {
	BaseURL   string
	HTTP      *http.Client
	UserAgent string
}

// New returns a client for the given base URL, or the default when empty.
func New(baseURL, userAgent string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		BaseURL:   strings.TrimRight(baseURL, "/"),
		HTTP:      &http.Client{Timeout: 30 * time.Second},
		UserAgent: userAgent,
	}
}

// Calculator is one calculator as the API describes it.
type Calculator struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	Summary     string         `json:"summary"`
	Tier        string         `json:"tier"`
	Description string         `json:"description"`
	Standard    string         `json:"standard"`
	Category    string         `json:"category"`
	WebPath     string         `json:"webPath"`
	Example     map[string]any `json:"example"`
	// Reference is the lookup data the fields draw on, such as the solar
	// zones and the design locations.
	Reference   map[string]any `json:"reference,omitempty"`
	InputSchema *Schema        `json:"inputSchema,omitempty"`
	// OutputSchema is kept as raw JSON: the CLI passes it through to MCP
	// clients and never reads it.
	OutputSchema json.RawMessage `json:"outputSchema,omitempty"`
}

// ToolName is the calculator's name as a tool: solar-pv-output becomes
// solar_pv_output, the same name the hosted MCP server uses.
func (c Calculator) ToolName() string { return strings.ReplaceAll(c.ID, "-", "_") }

// Schema is the part of a JSON Schema the CLI needs to read. The full schema
// is kept in Raw so it can be handed on unchanged.
type Schema struct {
	Type       any                `json:"type,omitempty"`
	Required   []string           `json:"required,omitempty"`
	Properties map[string]*Schema `json:"properties,omitempty"`
	Desc       string             `json:"description,omitempty"`
	Raw        json.RawMessage    `json:"-"`
}

// UnmarshalJSON keeps the raw schema alongside the parsed fields.
func (s *Schema) UnmarshalJSON(b []byte) error {
	type plain Schema
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*s = Schema(p)
	s.Raw = append(json.RawMessage(nil), b...)
	return nil
}

// TypeName is the schema's single type, ignoring "null".
func (s *Schema) TypeName() string {
	switch t := s.Type.(type) {
	case string:
		return t
	case []any:
		for _, v := range t {
			if name, ok := v.(string); ok && name != "null" {
				return name
			}
		}
	}
	return ""
}

// Result is a calculator's answer.
type Result struct {
	Calculator string          `json:"calculator"`
	Standard   string          `json:"standard"`
	Result     json.RawMessage `json:"result"`
	Summary    string          `json:"summary"`
	Notice     string          `json:"notice"`
}

// Document is a guide, article or question set, with or without its text.
type Document struct {
	Slug           string   `json:"slug"`
	Kind           string   `json:"kind"`
	Title          string   `json:"title"`
	Summary        string   `json:"summary"`
	Category       string   `json:"category"`
	Level          string   `json:"level,omitempty"`
	URL            string   `json:"url"`
	ReadingMinutes int      `json:"readingMinutes"`
	Calculators    []string `json:"calculators,omitempty"`
	Text           string   `json:"text,omitempty"`
}

// Error is an error the API returned, with any per-field problems.
type Error struct {
	Status  int
	Code    string `json:"code"`
	Message string `json:"message"`
	Fields  []struct {
		Field   string `json:"field"`
		Message string `json:"message"`
	} `json:"fields"`
}

func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString(e.Message)
	for _, f := range e.Fields {
		fmt.Fprintf(&b, "\n  %s: %s", f.Field, f.Message)
	}
	return b.String()
}

// Calculators lists every calculator, without schemas.
func (c *Client) Calculators(ctx context.Context) ([]Calculator, error) {
	var out struct {
		Calculators []Calculator `json:"calculators"`
	}
	err := c.get(ctx, "/calculators", nil, &out)
	return out.Calculators, err
}

// Calculator describes one calculator, with its input schema.
func (c *Client) Calculator(ctx context.Context, id string) (Calculator, error) {
	var out struct {
		Calculator Calculator `json:"calculator"`
	}
	err := c.get(ctx, "/calculators/"+url.PathEscape(id), nil, &out)
	return out.Calculator, err
}

// Run runs a calculator with its fields as query parameters. The server reads
// each value as the type its schema gives, so values are passed as typed. A
// run with no fields at all would describe the calculator instead, so it
// posts an empty request, which the server answers with what's missing.
func (c *Client) Run(ctx context.Context, id string, fields url.Values) (Result, error) {
	var out Result
	path := "/calculators/" + url.PathEscape(id)
	if len(fields) == 0 {
		return out, c.post(ctx, path, map[string]any{}, &out)
	}
	return out, c.get(ctx, path, fields, &out)
}

// RunJSON runs a calculator with its fields as a JSON object, which is how
// MCP clients send arguments.
func (c *Client) RunJSON(ctx context.Context, id string, args json.RawMessage) (Result, error) {
	var out Result
	if len(args) == 0 || string(args) == "null" {
		args = json.RawMessage("{}")
	}
	return out, c.post(ctx, "/calculators/"+url.PathEscape(id), args, &out)
}

// Search ranks documents against a query.
func (c *Client) Search(ctx context.Context, query string, limit int) ([]Document, error) {
	q := url.Values{"q": {query}}
	if limit > 0 {
		q.Set("limit", fmt.Sprint(limit))
	}
	var out struct {
		Results []Document `json:"results"`
	}
	err := c.get(ctx, "/documents/search", q, &out)
	return out.Results, err
}

// Documents lists documents, filtered by kind and category when given.
func (c *Client) Documents(ctx context.Context, kind, category string) ([]Document, error) {
	q := url.Values{}
	if kind != "" {
		q.Set("kind", kind)
	}
	if category != "" {
		q.Set("category", category)
	}
	var out struct {
		Documents []Document `json:"documents"`
	}
	err := c.get(ctx, "/documents", q, &out)
	return out.Documents, err
}

// Document fetches one document in full. kind is guide, article or faq, in
// either the singular or the plural.
func (c *Client) Document(ctx context.Context, kind, slug string) (Document, error) {
	var out struct {
		Document Document `json:"document"`
	}
	err := c.get(ctx, "/documents/"+url.PathEscape(kind)+"/"+url.PathEscape(slug), nil, &out)
	return out.Document, err
}

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	u := c.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	return c.do(req, out)
}

func (c *Client) post(ctx context.Context, path string, body any, out any) error {
	var payload []byte
	switch b := body.(type) {
	case json.RawMessage:
		payload = b
	default:
		var err error
		if payload, err = json.Marshal(b); err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, out)
}

func (c *Client) do(req *http.Request, out any) error {
	req.Header.Set("Accept", "application/json")
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach %s: %w", c.BaseURL, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		var e struct {
			Error *Error `json:"error"`
		}
		if json.Unmarshal(body, &e) == nil && e.Error != nil {
			e.Error.Status = resp.StatusCode
			return e.Error
		}
		return &Error{Status: resp.StatusCode, Message: fmt.Sprintf("the API answered %s", resp.Status)}
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("the API sent something unexpected: %w", err)
	}
	return nil
}

// IsNotFound reports whether err is the API saying a thing doesn't exist.
func IsNotFound(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Status == http.StatusNotFound
}
