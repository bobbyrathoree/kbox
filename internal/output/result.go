package output

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// DeployResult represents the result of a deploy operation
type DeployResult struct {
	Success    bool             `json:"success"`
	App        string           `json:"app"`
	Namespace  string           `json:"namespace"`
	Context    string           `json:"context,omitempty"`
	Resources  []ResourceResult `json:"resources"`
	Revision   int              `json:"revision,omitempty"`
	Error      string           `json:"error,omitempty"`
	DurationMs int64            `json:"duration_ms"`
}

// ResourceResult represents the result of applying a single resource
type ResourceResult struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Action string `json:"action"` // created, updated, unchanged
	Error  string `json:"error,omitempty"`
}

// PreviewResult represents the result of a preview operation
type PreviewResult struct {
	Success   bool   `json:"success"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	App       string `json:"app"`
	Action    string `json:"action"` // created, destroyed
	Error     string `json:"error,omitempty"`
}

// ListResult represents a list of items
type ListResult struct {
	Success bool        `json:"success"`
	Items   interface{} `json:"items"`
	Count   int         `json:"count"`
	Error   string      `json:"error,omitempty"`
}

// Writer handles output in different formats
type Writer struct {
	out    io.Writer
	format string // "text" or "json"
	ciMode bool
}

// NewWriter creates a new output writer
func NewWriter(out io.Writer, format string, ciMode bool) *Writer {
	if format == "" {
		format = "text"
	}
	return &Writer{
		out:    out,
		format: format,
		ciMode: ciMode,
	}
}

// WriteDeployResult writes a deploy result
func (w *Writer) WriteDeployResult(result *DeployResult) error {
	if w.format == "json" {
		return json.NewEncoder(w.out).Encode(result)
	}

	// Text format
	if !w.ciMode {
		// Normal text output is handled by the command itself
		return nil
	}

	// CI mode text: minimal output
	if result.Success {
		fmt.Fprintf(w.out, "Deployed %s to %s (revision %d)\n", result.App, result.Namespace, result.Revision)
	} else {
		fmt.Fprintf(w.out, "Deploy failed: %s\n", result.Error)
	}
	return nil
}

// WritePreviewResult writes a preview result
func (w *Writer) WritePreviewResult(result *PreviewResult) error {
	if w.format == "json" {
		return json.NewEncoder(w.out).Encode(result)
	}

	// Text format handled by command
	return nil
}

// WriteJSON writes any result as JSON
func (w *Writer) WriteJSON(result interface{}) error {
	return json.NewEncoder(w.out).Encode(result)
}

// IsJSON returns true if output format is JSON
func (w *Writer) IsJSON() bool {
	return w.format == "json"
}

// IsCIMode returns true if CI mode is enabled
func (w *Writer) IsCIMode() bool {
	return w.ciMode
}

// Timer helps track operation duration
type Timer struct {
	start time.Time
}

// NewTimer creates a new timer
func NewTimer() *Timer {
	return &Timer{start: time.Now()}
}

// ElapsedMs returns elapsed time in milliseconds
func (t *Timer) ElapsedMs() int64 {
	return time.Since(t.start).Milliseconds()
}
