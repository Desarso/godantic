package common_tools

// Core workflow logic shared by the agent tools (workflows.go) and the REST
// handlers of the host application. Everything here takes plain parameters and
// returns structs/errors; formatting for the LLM lives in workflows.go.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
)

// workflowsDir is where workflow data lives, relative to the server's cwd.
// It is a var so tests can point it at a temp dir.
var workflowsDir = "data/workflows"

// WorkflowEnvHook, when set by the host application, returns extra environment
// variables for a workflow run (e.g. a short-lived API token scoped to the
// workflow owner). It cannot override the reserved WORKFLOW_* / AGENT_* keys.
var WorkflowEnvHook func(workflowID, ownerUID string, timeout time.Duration) map[string]string

// SetWorkflowsDir overrides where workflow data lives (e.g. a persistent data
// volume). Call once at startup before any workflow is created or scheduled.
func SetWorkflowsDir(dir string) {
	if strings.TrimSpace(dir) != "" {
		workflowsDir = dir
	}
}

// WorkflowsDir returns the directory where workflow data lives.
func WorkflowsDir() string {
	return workflowsDir
}

const (
	DefaultWorkflowTimeoutSeconds = 30 * 60
	MaxWorkflowTimeoutSeconds     = 6 * 60 * 60
	MinWorkflowTimeoutSeconds     = 10
	MaxWorkflowCodeBytes          = 50000
	MaxWorkflowNameLength         = 200
	MaxWorkflowDescriptionLength  = 2000
	workflowMaxLogBytes           = 5 * 1024 * 1024 // logs.txt (enforced by the executor)
	workflowMaxStdioBytes         = 1 * 1024 * 1024 // stdout.txt / stderr.txt (enforced here)
	workflowExecutorScript        = "workflow_executor.ts"
)

var (
	ErrWorkflowNotFound   = errors.New("workflow not found")
	ErrWorkflowForbidden  = errors.New("you do not have access to this workflow")
	ErrWorkflowRunning    = errors.New("workflow is running")
	ErrWorkflowNotRunning = errors.New("workflow is not running")
	ErrInvalidWorkflowID  = errors.New("invalid workflow id")
	ErrInvalidWorkflow    = errors.New("invalid workflow")
)

var workflowIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// ValidateWorkflowID rejects anything that is not a plain identifier, which
// prevents path traversal via the id (e.g. "../../etc").
func ValidateWorkflowID(id string) error {
	if !workflowIDPattern.MatchString(id) {
		return fmt.Errorf("%w: %q", ErrInvalidWorkflowID, id)
	}
	return nil
}

// WorkflowStatus represents the status of a workflow (status.json)
type WorkflowStatus struct {
	Result      json.RawMessage `json:"result,omitempty"`
	TaskSuccess *bool           `json:"task_success,omitempty"`
	ID          string          `json:"id"`
	Status      string          `json:"status"` // "pending", "running", "completed", "failed"
	StartedAt   string          `json:"started_at,omitempty"`
	CompletedAt string          `json:"completed_at,omitempty"`
	Error       string          `json:"error,omitempty"`
	PID         int             `json:"pid,omitempty"`
	PGID        int             `json:"pgid,omitempty"`
	ExitCode    *int            `json:"exit_code,omitempty"`
	Trigger     string          `json:"trigger,omitempty"` // "manual", "agent", "schedule"
}

// WorkflowSchedule represents scheduling configuration for a workflow (schedule.json)
type WorkflowSchedule struct {
	Enabled     bool   `json:"enabled"`
	Type        string `json:"type"`                   // "cron", "once", "interval"
	Cron        string `json:"cron,omitempty"`         // Cron expression (5 or 6 fields)
	RunAt       string `json:"run_at,omitempty"`       // RFC3339 timestamp for one-time runs
	IntervalSec int    `json:"interval_sec,omitempty"` // Interval in seconds for repeated runs
	LastRun     string `json:"last_run,omitempty"`
	NextRun     string `json:"next_run,omitempty"`
	CronEntryID int    `json:"cron_entry_id,omitempty"`
}

// WorkflowMetadata is persisted in metadata.json. Legacy files only contain
// id/name/created_at (all strings), which unmarshal fine into this struct.
type WorkflowMetadata struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Description    string `json:"description,omitempty"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at,omitempty"`
	OwnerUID       string `json:"owner_uid,omitempty"`
	OwnerEmail     string `json:"owner_email,omitempty"`
	CreatedBy      string `json:"created_by,omitempty"` // "agent" or "user"
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

// WorkflowInfo is kept for backwards compatibility.
type WorkflowInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	StartedAt   string `json:"started_at,omitempty"`
	CompletedAt string `json:"completed_at,omitempty"`
	Error       string `json:"error,omitempty"`
	CreatedAt   string `json:"created_at"`
}

// WorkflowDetails is the aggregated view of a workflow used by the REST API.
type WorkflowDetails struct {
	Result                json.RawMessage   `json:"result,omitempty"`
	TaskSuccess           *bool             `json:"task_success,omitempty"`
	ID                    string            `json:"id"`
	Name                  string            `json:"name"`
	Description           string            `json:"description,omitempty"`
	Status                string            `json:"status"`
	StartedAt             string            `json:"started_at,omitempty"`
	CompletedAt           string            `json:"completed_at,omitempty"`
	Error                 string            `json:"error,omitempty"`
	PID                   int               `json:"pid,omitempty"`
	ExitCode              *int              `json:"exit_code,omitempty"`
	Trigger               string            `json:"trigger,omitempty"`
	CreatedAt             string            `json:"created_at"`
	UpdatedAt             string            `json:"updated_at,omitempty"`
	OwnerUID              string            `json:"owner_uid,omitempty"`
	OwnerEmail            string            `json:"owner_email,omitempty"`
	CreatedBy             string            `json:"created_by,omitempty"`
	TimeoutSeconds        int               `json:"timeout_seconds"`
	Schedule              *WorkflowSchedule `json:"schedule,omitempty"`
	Code                  string            `json:"code,omitempty"`
	CanManage             bool              `json:"can_manage"`
	DefaultTimeoutSeconds int               `json:"default_timeout_seconds"`
	MaxTimeoutSeconds     int               `json:"max_timeout_seconds"`
}

// WorkflowActor identifies who is acting on workflows.
type WorkflowActor struct {
	UID     string
	Email   string
	IsAdmin bool // super admin: sees and manages everything
}

// SystemWorkflowActor is used for unscoped callers (legacy tool calls with no
// user context, the scheduler).
var SystemWorkflowActor = WorkflowActor{IsAdmin: true}

// CanAccess reports whether the actor may see/manage a workflow. Workflows
// without an owner are legacy/shared and accessible to everyone.
func (a WorkflowActor) CanAccess(m *WorkflowMetadata) bool {
	if a.IsAdmin || m == nil {
		return true
	}
	if m.OwnerUID == "" && m.OwnerEmail == "" {
		return true
	}
	if a.UID != "" && m.OwnerUID == a.UID {
		return true
	}
	if a.Email != "" && m.OwnerEmail != "" && strings.EqualFold(a.Email, m.OwnerEmail) {
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// File helpers
// ---------------------------------------------------------------------------

func ensureWorkflowsDir() error {
	return os.MkdirAll(workflowsDir, 0755)
}

// workflowDir validates the id and returns its directory (may not exist).
func workflowDir(id string) (string, error) {
	if err := ValidateWorkflowID(id); err != nil {
		return "", err
	}
	return filepath.Join(workflowsDir, id), nil
}

// existingWorkflowDir validates the id and checks that the workflow exists.
func existingWorkflowDir(id string) (string, error) {
	dir, err := workflowDir(id)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return "", fmt.Errorf("%w: '%s'", ErrWorkflowNotFound, id)
	}
	return dir, nil
}

func writeJSONAtomic(path string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.tmp-%d", path, time.Now().UnixNano())
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readWorkflowStatus(dir string) *WorkflowStatus {
	data, err := os.ReadFile(filepath.Join(dir, "status.json"))
	if err != nil {
		return nil
	}
	var st WorkflowStatus
	if json.Unmarshal(data, &st) != nil {
		return nil
	}
	return &st
}

func writeWorkflowStatus(dir string, st *WorkflowStatus) error {
	return writeJSONAtomic(filepath.Join(dir, "status.json"), st)
}

func readWorkflowSchedule(dir string) *WorkflowSchedule {
	data, err := os.ReadFile(filepath.Join(dir, "schedule.json"))
	if err != nil {
		return nil
	}
	var s WorkflowSchedule
	if json.Unmarshal(data, &s) != nil {
		return nil
	}
	return &s
}

func appendWorkflowLog(dir, message string) {
	f, err := os.OpenFile(filepath.Join(dir, "logs.txt"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "[%s] %s\n", time.Now().UTC().Format("2006-01-02T15:04:05.000Z"), message)
}

// LoadWorkflowMetadata reads metadata.json for a workflow.
func LoadWorkflowMetadata(id string) (*WorkflowMetadata, error) {
	dir, err := existingWorkflowDir(id)
	if err != nil {
		return nil, err
	}
	meta := &WorkflowMetadata{ID: id}
	data, err := os.ReadFile(filepath.Join(dir, "metadata.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return meta, nil
		}
		return nil, fmt.Errorf("failed to read workflow metadata: %v", err)
	}
	if err := json.Unmarshal(data, meta); err != nil {
		return nil, fmt.Errorf("failed to parse workflow metadata: %v", err)
	}
	meta.ID = id
	return meta, nil
}

func saveWorkflowMetadata(dir string, meta *WorkflowMetadata) error {
	return writeJSONAtomic(filepath.Join(dir, "metadata.json"), meta)
}

// CanManage reports whether the actor may change a workflow (edit, run, stop,
// schedule, delete). Legacy workflows without an owner are visible to everyone
// but only admins may change them, since their schedules run for all users.
func (a WorkflowActor) CanManage(m *WorkflowMetadata) bool {
	if a.IsAdmin || m == nil {
		return true
	}
	if m.OwnerUID == "" && m.OwnerEmail == "" {
		return false
	}
	return a.CanAccess(m)
}

// AuthorizeWorkflow checks that the workflow exists and that the actor may view it.
func AuthorizeWorkflow(id string, actor WorkflowActor) (*WorkflowMetadata, error) {
	meta, err := LoadWorkflowMetadata(id)
	if err != nil {
		return nil, err
	}
	if !actor.CanAccess(meta) {
		return nil, fmt.Errorf("%w: '%s'", ErrWorkflowForbidden, id)
	}
	return meta, nil
}

// AuthorizeWorkflowManage checks that the workflow exists and that the actor may change it.
func AuthorizeWorkflowManage(id string, actor WorkflowActor) (*WorkflowMetadata, error) {
	meta, err := AuthorizeWorkflow(id, actor)
	if err != nil {
		return nil, err
	}
	if !actor.CanManage(meta) {
		return nil, fmt.Errorf("%w: '%s' is a shared workflow; only an admin can change it", ErrWorkflowForbidden, id)
	}
	return meta, nil
}

// EffectiveWorkflowTimeout returns the timeout applied to a run.
func EffectiveWorkflowTimeout(meta *WorkflowMetadata) time.Duration {
	secs := DefaultWorkflowTimeoutSeconds
	if meta != nil && meta.TimeoutSeconds > 0 {
		secs = meta.TimeoutSeconds
	}
	if secs > MaxWorkflowTimeoutSeconds {
		secs = MaxWorkflowTimeoutSeconds
	}
	if secs < MinWorkflowTimeoutSeconds {
		secs = MinWorkflowTimeoutSeconds
	}
	return time.Duration(secs) * time.Second
}

func validateTimeoutSeconds(secs int) error {
	if secs == 0 {
		return nil
	}
	if secs < MinWorkflowTimeoutSeconds || secs > MaxWorkflowTimeoutSeconds {
		return fmt.Errorf("%w: timeout_seconds must be between %d and %d", ErrInvalidWorkflow, MinWorkflowTimeoutSeconds, MaxWorkflowTimeoutSeconds)
	}
	return nil
}

func validateWorkflowCode(code string) error {
	if strings.TrimSpace(code) == "" {
		return fmt.Errorf("%w: code cannot be empty", ErrInvalidWorkflow)
	}
	if len(code) > MaxWorkflowCodeBytes {
		return fmt.Errorf("%w: code exceeds %d bytes", ErrInvalidWorkflow, MaxWorkflowCodeBytes)
	}
	return nil
}

func validateWorkflowName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%w: name cannot be empty", ErrInvalidWorkflow)
	}
	if len(name) > MaxWorkflowNameLength {
		return fmt.Errorf("%w: name exceeds %d characters", ErrInvalidWorkflow, MaxWorkflowNameLength)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Create / update / read
// ---------------------------------------------------------------------------

// CreateWorkflowParams are the inputs for CreateWorkflow.
type CreateWorkflowParams struct {
	Name           string
	Description    string
	Code           string
	TimeoutSeconds int
	OwnerUID       string
	OwnerEmail     string
	CreatedBy      string // "agent" or "user"
}

// CreateWorkflow persists a new workflow and returns its details.
func CreateWorkflow(p CreateWorkflowParams) (*WorkflowDetails, error) {
	ensureWorkflowsInit()
	p.Name = strings.TrimSpace(p.Name)
	if err := validateWorkflowName(p.Name); err != nil {
		return nil, err
	}
	if err := validateWorkflowCode(p.Code); err != nil {
		return nil, err
	}
	if len(p.Description) > MaxWorkflowDescriptionLength {
		return nil, fmt.Errorf("%w: description exceeds %d characters", ErrInvalidWorkflow, MaxWorkflowDescriptionLength)
	}
	if err := validateTimeoutSeconds(p.TimeoutSeconds); err != nil {
		return nil, err
	}
	if err := ensureWorkflowsDir(); err != nil {
		return nil, fmt.Errorf("failed to create workflows directory: %v", err)
	}

	var id, dir string
	for attempt := 0; attempt < 5; attempt++ {
		id = uuid.New().String()[:8]
		dir = filepath.Join(workflowsDir, id)
		if err := os.Mkdir(dir, 0755); err == nil {
			break
		} else if !os.IsExist(err) {
			return nil, fmt.Errorf("failed to create workflow directory: %v", err)
		}
		dir = ""
	}
	if dir == "" {
		return nil, fmt.Errorf("failed to allocate a workflow id")
	}

	if err := os.WriteFile(filepath.Join(dir, "code.ts"), []byte(p.Code), 0644); err != nil {
		return nil, fmt.Errorf("failed to save workflow code: %v", err)
	}
	now := time.Now().Format(time.RFC3339)
	meta := &WorkflowMetadata{
		ID:             id,
		Name:           p.Name,
		Description:    strings.TrimSpace(p.Description),
		CreatedAt:      now,
		UpdatedAt:      now,
		OwnerUID:       p.OwnerUID,
		OwnerEmail:     p.OwnerEmail,
		CreatedBy:      p.CreatedBy,
		TimeoutSeconds: p.TimeoutSeconds,
	}
	if err := saveWorkflowMetadata(dir, meta); err != nil {
		return nil, fmt.Errorf("failed to save workflow metadata: %v", err)
	}
	if err := writeWorkflowStatus(dir, &WorkflowStatus{ID: id, Status: "pending"}); err != nil {
		return nil, fmt.Errorf("failed to save workflow status: %v", err)
	}
	return GetWorkflow(id, true)
}

// UpdateWorkflowParams: nil fields are left unchanged.
type UpdateWorkflowParams struct {
	Name           *string
	Description    *string
	Code           *string
	TimeoutSeconds *int
}

// UpdateWorkflow changes metadata and/or code. Refuses while the workflow runs.
// Returns the list of changed fields.
func UpdateWorkflow(id string, p UpdateWorkflowParams) ([]string, error) {
	dir, err := existingWorkflowDir(id)
	if err != nil {
		return nil, err
	}
	if IsWorkflowRunning(id) {
		return nil, fmt.Errorf("%w: cannot edit workflow '%s' while it is running; stop it first", ErrWorkflowRunning, id)
	}
	meta, err := LoadWorkflowMetadata(id)
	if err != nil {
		return nil, err
	}

	var changes []string
	if p.Name != nil {
		name := strings.TrimSpace(*p.Name)
		if err := validateWorkflowName(name); err != nil {
			return nil, err
		}
		if name != meta.Name {
			meta.Name = name
			changes = append(changes, "name")
		}
	}
	if p.Description != nil {
		desc := strings.TrimSpace(*p.Description)
		if len(desc) > MaxWorkflowDescriptionLength {
			return nil, fmt.Errorf("%w: description exceeds %d characters", ErrInvalidWorkflow, MaxWorkflowDescriptionLength)
		}
		if desc != meta.Description {
			meta.Description = desc
			changes = append(changes, "description")
		}
	}
	if p.TimeoutSeconds != nil {
		if err := validateTimeoutSeconds(*p.TimeoutSeconds); err != nil {
			return nil, err
		}
		if *p.TimeoutSeconds != meta.TimeoutSeconds {
			meta.TimeoutSeconds = *p.TimeoutSeconds
			changes = append(changes, "timeout_seconds")
		}
	}
	codeChanged := false
	if p.Code != nil {
		if err := validateWorkflowCode(*p.Code); err != nil {
			return nil, err
		}
		old, _ := os.ReadFile(filepath.Join(dir, "code.ts"))
		if string(old) != *p.Code {
			if err := os.WriteFile(filepath.Join(dir, "code.ts"), []byte(*p.Code), 0644); err != nil {
				return nil, fmt.Errorf("failed to update workflow code: %v", err)
			}
			codeChanged = true
			changes = append(changes, "code")
		}
	}
	if len(changes) == 0 {
		return changes, nil
	}
	meta.UpdatedAt = time.Now().Format(time.RFC3339)
	if err := saveWorkflowMetadata(dir, meta); err != nil {
		return nil, fmt.Errorf("failed to update workflow metadata: %v", err)
	}
	if codeChanged {
		if err := writeWorkflowStatus(dir, &WorkflowStatus{ID: id, Status: "pending"}); err != nil {
			return nil, fmt.Errorf("failed to reset workflow status: %v", err)
		}
	}
	return changes, nil
}

// PatchWorkflowCode performs find/replace on the code; returns the number of replacements.
func PatchWorkflowCode(id, find, replace string, replaceAll bool) (int, error) {
	if find == "" {
		return 0, fmt.Errorf("find string cannot be empty")
	}
	dir, err := existingWorkflowDir(id)
	if err != nil {
		return 0, err
	}
	codeBytes, err := os.ReadFile(filepath.Join(dir, "code.ts"))
	if err != nil {
		return 0, fmt.Errorf("failed to read workflow code: %v", err)
	}
	code := string(codeBytes)
	count := strings.Count(code, find)
	if count == 0 {
		return 0, fmt.Errorf("find string not found in workflow code")
	}
	var newCode string
	if replaceAll {
		newCode = strings.ReplaceAll(code, find, replace)
	} else {
		newCode = strings.Replace(code, find, replace, 1)
		count = 1
	}
	if _, err := UpdateWorkflow(id, UpdateWorkflowParams{Code: &newCode}); err != nil {
		return 0, err
	}
	return count, nil
}

// GetWorkflowCode returns the raw code of a workflow.
func GetWorkflowCode(id string) (string, error) {
	dir, err := existingWorkflowDir(id)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(dir, "code.ts"))
	if err != nil {
		return "", fmt.Errorf("failed to read workflow code: %v", err)
	}
	return string(data), nil
}

// GetWorkflow returns the aggregated details of a workflow.
func GetWorkflow(id string, includeCode bool) (*WorkflowDetails, error) {
	dir, err := existingWorkflowDir(id)
	if err != nil {
		return nil, err
	}
	meta, err := LoadWorkflowMetadata(id)
	if err != nil {
		meta = &WorkflowMetadata{ID: id}
	}
	d := &WorkflowDetails{
		ID:                    id,
		Name:                  meta.Name,
		Description:           meta.Description,
		CreatedAt:             meta.CreatedAt,
		UpdatedAt:             meta.UpdatedAt,
		OwnerUID:              meta.OwnerUID,
		OwnerEmail:            meta.OwnerEmail,
		CreatedBy:             meta.CreatedBy,
		TimeoutSeconds:        int(EffectiveWorkflowTimeout(meta) / time.Second),
		DefaultTimeoutSeconds: DefaultWorkflowTimeoutSeconds,
		MaxTimeoutSeconds:     MaxWorkflowTimeoutSeconds,
		CanManage:             true,
	}
	if st := readWorkflowStatus(dir); st != nil {
		d.Result = st.Result
		d.TaskSuccess = st.TaskSuccess
		d.Status = st.Status
		d.StartedAt = st.StartedAt
		d.CompletedAt = st.CompletedAt
		d.Error = st.Error
		d.PID = st.PID
		d.ExitCode = st.ExitCode
		d.Trigger = st.Trigger
	}
	if d.Status == "" {
		d.Status = "pending"
	}
	// status.json can lag behind reality (e.g. crashed executor); the in-memory
	// process table is authoritative for "running".
	if IsWorkflowRunning(id) {
		d.Status = "running"
	} else if d.Status == "running" {
		d.Status = "failed"
		if d.Error == "" {
			d.Error = "Workflow process is no longer running"
		}
	}
	if s := readWorkflowSchedule(dir); s != nil {
		d.Schedule = s
	}
	if includeCode {
		if code, err := os.ReadFile(filepath.Join(dir, "code.ts")); err == nil {
			d.Code = string(code)
		}
	}
	return d, nil
}

// ListWorkflows returns the workflows visible to the actor, newest first.
func ListWorkflows(actor WorkflowActor) ([]WorkflowDetails, error) {
	ensureWorkflowsInit()
	if err := ensureWorkflowsDir(); err != nil {
		return nil, fmt.Errorf("failed to access workflows directory: %v", err)
	}
	entries, err := os.ReadDir(workflowsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to list workflows: %v", err)
	}
	out := make([]WorkflowDetails, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || ValidateWorkflowID(entry.Name()) != nil {
			continue
		}
		meta, err := LoadWorkflowMetadata(entry.Name())
		if err != nil || !actor.CanAccess(meta) {
			continue
		}
		d, err := GetWorkflow(entry.Name(), false)
		if err != nil {
			continue
		}
		d.CanManage = actor.CanManage(meta)
		out = append(out, *d)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out, nil
}

// ReadWorkflowLogs returns logs.txt, optionally only the last tailLines lines.
func ReadWorkflowLogs(id string, tailLines int) (string, error) {
	dir, err := existingWorkflowDir(id)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(dir, "logs.txt"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("failed to read workflow logs: %v", err)
	}
	logs := string(data)
	if tailLines > 0 {
		lines := strings.Split(logs, "\n")
		if len(lines) > tailLines {
			lines = lines[len(lines)-tailLines:]
		}
		logs = strings.Join(lines, "\n")
	}
	return logs, nil
}

// ---------------------------------------------------------------------------
// Process management
// ---------------------------------------------------------------------------

type runningWorkflow struct {
	pid        int
	done       chan struct{}
	stopReason string
	timer      *time.Timer
}

var (
	runningMu        sync.Mutex
	runningWorkflows = map[string]*runningWorkflow{}

	// Test seams.
	findWorkflowRunner      = findTypeScriptRunner
	workflowTimeoutOverride time.Duration
)

// IsWorkflowRunning reports whether this server process is running the workflow.
// Orphans from a previous server process are killed by InitWorkflows, so the
// in-memory table is authoritative.
func IsWorkflowRunning(id string) bool {
	runningMu.Lock()
	defer runningMu.Unlock()
	_, ok := runningWorkflows[id]
	return ok
}

// cappedFile writes up to limit bytes and silently drops the rest.
type cappedFile struct {
	f         *os.File
	remaining int64
	truncated bool
}

func newCappedFile(path string, limit int64) (*cappedFile, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	return &cappedFile{f: f, remaining: limit}, nil
}

func (c *cappedFile) Write(p []byte) (int, error) {
	n := len(p)
	if c.remaining <= 0 {
		if !c.truncated {
			c.truncated = true
			_, _ = c.f.WriteString("\n[output truncated: size limit reached]\n")
		}
		return n, nil
	}
	chunk := p
	if int64(len(chunk)) > c.remaining {
		chunk = chunk[:c.remaining]
	}
	w, err := c.f.Write(chunk)
	c.remaining -= int64(w)
	if err != nil {
		return w, err
	}
	return n, nil
}

func (c *cappedFile) Close() error { return c.f.Close() }

var _ io.WriteCloser = (*cappedFile)(nil)

// RunWorkflow starts a workflow as a background process group and returns its pid.
// trigger is recorded in status.json ("manual", "agent", "schedule").
func RunWorkflow(id string, trigger string) (int, error) {
	ensureWorkflowsInit()
	dir, err := existingWorkflowDir(id)
	if err != nil {
		return 0, err
	}
	meta, err := LoadWorkflowMetadata(id)
	if err != nil {
		return 0, err
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return 0, err
	}
	codePath := filepath.Join(absDir, "code.ts")
	codeStat, err := os.Stat(codePath)
	if err != nil {
		return 0, fmt.Errorf("failed to read workflow code: %v", err)
	}
	if codeStat.Size() == 0 || codeStat.Size() > MaxWorkflowCodeBytes {
		return 0, fmt.Errorf("%w: code must be between 1 and %d bytes", ErrInvalidWorkflow, MaxWorkflowCodeBytes)
	}

	runner, err := findWorkflowRunner()
	if err != nil {
		return 0, err
	}

	runningMu.Lock()
	defer runningMu.Unlock()
	if rw, ok := runningWorkflows[id]; ok {
		return 0, fmt.Errorf("%w: workflow '%s' is already running (PID: %d)", ErrWorkflowRunning, id, rw.pid)
	}

	timeout := EffectiveWorkflowTimeout(meta)
	if workflowTimeoutOverride > 0 {
		timeout = workflowTimeoutOverride
	}

	// Fresh logs for each run.
	_ = os.WriteFile(filepath.Join(absDir, "logs.txt"), nil, 0644)
	stdoutFile, err := newCappedFile(filepath.Join(absDir, "stdout.txt"), workflowMaxStdioBytes)
	if err != nil {
		return 0, fmt.Errorf("failed to create stdout file: %v", err)
	}
	stderrFile, err := newCappedFile(filepath.Join(absDir, "stderr.txt"), workflowMaxStdioBytes)
	if err != nil {
		stdoutFile.Close()
		return 0, fmt.Errorf("failed to create stderr file: %v", err)
	}

	extraEnv := map[string]string{
		"WORKFLOW_ID":              id,
		"WORKFLOW_DIR":             absDir,
		"WORKFLOW_TIMEOUT_SECONDS": strconv.Itoa(int(timeout / time.Second)),
		"WORKFLOW_MAX_LOG_BYTES":   strconv.Itoa(workflowMaxLogBytes),
		"AGENT_CONVERSATION_ID":    "workflow-" + id,
		"AGENT_USER_WORKSPACE_ID":  meta.OwnerUID,
	}
	if WorkflowEnvHook != nil {
		for k, v := range WorkflowEnvHook(id, meta.OwnerUID, timeout) {
			if _, reserved := extraEnv[k]; !reserved {
				extraEnv[k] = v
			}
		}
	}

	args := append(append([]string{}, runner.args...), workflowExecutorScript, id, codePath)
	cmd := exec.Command(runner.command, args...)
	cmd.Dir = runner.dir
	cmd.Env = BuildWorkflowEnv(os.Getenv, extraEnv)
	cmd.Stdout = stdoutFile
	cmd.Stderr = stderrFile
	setWorkflowProcessGroup(cmd)

	startedAt := time.Now().Format(time.RFC3339)
	if err := cmd.Start(); err != nil {
		stdoutFile.Close()
		stderrFile.Close()
		_ = writeWorkflowStatus(absDir, &WorkflowStatus{
			ID: id, Status: "failed", StartedAt: startedAt, CompletedAt: startedAt,
			Error: fmt.Sprintf("failed to start workflow: %v", err), Trigger: trigger,
		})
		return 0, fmt.Errorf("failed to start workflow: %v", err)
	}

	pid := cmd.Process.Pid
	_ = writeWorkflowStatus(absDir, &WorkflowStatus{
		ID: id, Status: "running", StartedAt: startedAt, PID: pid, PGID: pid, Trigger: trigger,
	})
	appendWorkflowLog(absDir, fmt.Sprintf("Run started (trigger: %s, timeout: %s)", trigger, timeout))

	rw := &runningWorkflow{pid: pid, done: make(chan struct{})}
	rw.timer = time.AfterFunc(timeout, func() {
		stopRunningWorkflow(id, fmt.Sprintf("Timed out after %s", timeout))
	})
	runningWorkflows[id] = rw

	go waitForWorkflow(id, absDir, cmd, rw, stdoutFile, stderrFile)
	return pid, nil
}

func waitForWorkflow(id, dir string, cmd *exec.Cmd, rw *runningWorkflow, stdoutFile, stderrFile *cappedFile) {
	waitErr := cmd.Wait()
	rw.timer.Stop()
	stdoutFile.Close()
	stderrFile.Close()

	// Reap anything the executor left behind in its process group.
	_ = killWorkflowProcessGroup(rw.pid)

	exitCode := -1
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}

	runningMu.Lock()
	reason := rw.stopReason
	delete(runningWorkflows, id)
	runningMu.Unlock()

	// The workflow may have been deleted while running.
	if _, err := os.Stat(dir); err != nil {
		close(rw.done)
		return
	}

	st := readWorkflowStatus(dir)
	if st == nil {
		st = &WorkflowStatus{ID: id}
	}
	now := time.Now().Format(time.RFC3339)
	switch {
	case reason != "":
		st.Status = "failed"
		st.Error = reason
		st.CompletedAt = now
		appendWorkflowLog(dir, "Workflow stopped: "+reason)
	case st.Status == "running" || st.Status == "" || st.Status == "pending":
		st.Status = "failed"
		msg := fmt.Sprintf("Workflow process exited unexpectedly (exit code %d)", exitCode)
		if waitErr != nil && exitCode == -1 {
			msg = fmt.Sprintf("Workflow process exited unexpectedly: %v", waitErr)
		}
		if tail := tailFile(filepath.Join(dir, "stderr.txt"), 800); tail != "" {
			msg += ": " + tail
		}
		st.Error = msg
		st.CompletedAt = now
		appendWorkflowLog(dir, "ERROR: "+msg)
	}
	if st.CompletedAt == "" {
		st.CompletedAt = now
	}
	st.ExitCode = &exitCode
	_ = writeWorkflowStatus(dir, st)
	close(rw.done)
}

func tailFile(path string, maxBytes int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	s := strings.TrimSpace(string(data))
	if len(s) > maxBytes {
		s = "..." + s[len(s)-maxBytes:]
	}
	return s
}

// stopRunningWorkflow kills a tracked workflow; returns its done channel or nil.
func stopRunningWorkflow(id, reason string) <-chan struct{} {
	runningMu.Lock()
	rw, ok := runningWorkflows[id]
	if !ok {
		runningMu.Unlock()
		return nil
	}
	if rw.stopReason == "" {
		rw.stopReason = reason
	}
	pid := rw.pid
	runningMu.Unlock()

	if err := killWorkflowProcessGroup(pid); err != nil {
		log.Printf("[workflows] failed to kill workflow %s (pgid %d): %v", id, pid, err)
	}
	return rw.done
}

// StopWorkflow kills a running workflow's whole process group and waits
// briefly for it to exit.
func StopWorkflow(id string) error {
	dir, err := existingWorkflowDir(id)
	if err != nil {
		return err
	}
	if done := stopRunningWorkflow(id, "Stopped by user"); done != nil {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
		return nil
	}
	// Not tracked: clean up a stale "running" status if present.
	if st := readWorkflowStatus(dir); st != nil && st.Status == "running" {
		st.Status = "failed"
		st.Error = "Workflow process is no longer running"
		st.CompletedAt = time.Now().Format(time.RFC3339)
		_ = writeWorkflowStatus(dir, st)
	}
	return fmt.Errorf("%w: '%s'", ErrWorkflowNotRunning, id)
}

// DeleteWorkflow unschedules, stops (if running) and removes a workflow.
func DeleteWorkflow(id string) error {
	dir, err := existingWorkflowDir(id)
	if err != nil {
		return err
	}
	unregisterSchedule(id)
	if done := stopRunningWorkflow(id, "Workflow deleted"); done != nil {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("failed to delete workflow: %v", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Scheduling
// ---------------------------------------------------------------------------

var (
	schedulerMu    sync.Mutex
	scheduler      *cron.Cron
	cronEntries    = map[string]cron.EntryID{}
	onceTimers     = map[string]*time.Timer{}
	workflowsInit  sync.Once
	cronSpecParser = cron.NewParser(cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
)

// getScheduler returns the cron scheduler. Caller must hold schedulerMu.
func getSchedulerLocked() *cron.Cron {
	if scheduler == nil {
		scheduler = cron.New(cron.WithParser(cronSpecParser))
		scheduler.Start()
	}
	return scheduler
}

// InitWorkflows must be called once at server startup. It reconciles stale
// "running" statuses left by a previous server process (killing orphaned
// executors, since they can no longer be tracked or time-limited) and
// registers all persisted schedules. Safe to call multiple times.
func InitWorkflows() {
	workflowsInit.Do(func() {
		reconcileWorkflowStatuses()
		loadExistingSchedules()
	})
}

// ensureWorkflowsInit lazily initializes if the host app did not call InitWorkflows.
func ensureWorkflowsInit() { InitWorkflows() }

func reconcileWorkflowStatuses() {
	entries, err := os.ReadDir(workflowsDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || ValidateWorkflowID(entry.Name()) != nil {
			continue
		}
		dir := filepath.Join(workflowsDir, entry.Name())
		st := readWorkflowStatus(dir)
		if st == nil || st.Status != "running" {
			continue
		}
		if st.PGID > 0 && workflowProcessLooksLikeOurs(st.PGID) {
			_ = killWorkflowProcessGroup(st.PGID)
		} else if st.PID > 0 && workflowProcessLooksLikeOurs(st.PID) {
			_ = killWorkflowProcessGroup(st.PID)
		}
		st.Status = "failed"
		st.Error = "Interrupted: server restarted while the workflow was running"
		st.CompletedAt = time.Now().Format(time.RFC3339)
		_ = writeWorkflowStatus(dir, st)
		appendWorkflowLog(dir, "ERROR: "+st.Error)
	}
}

func loadExistingSchedules() {
	entries, err := os.ReadDir(workflowsDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		id := entry.Name()
		if !entry.IsDir() || ValidateWorkflowID(id) != nil {
			continue
		}
		dir := filepath.Join(workflowsDir, id)
		schedule := readWorkflowSchedule(dir)
		if schedule == nil || !schedule.Enabled {
			continue
		}
		if err := registerSchedule(id, schedule); err != nil {
			log.Printf("[workflows] could not restore schedule for %s: %v", id, err)
			if schedule.Type == "once" {
				disableSchedule(id)
			}
			continue
		}
		_ = writeJSONAtomic(filepath.Join(dir, "schedule.json"), schedule)
	}
}

// ParseWorkflowSchedule validates a schedule request (same format as the
// Schedule_Workflow tool) and returns the schedule to persist.
func ParseWorkflowSchedule(scheduleType, value string) (*WorkflowSchedule, error) {
	value = strings.TrimSpace(value)
	schedule := &WorkflowSchedule{Enabled: true, Type: scheduleType}
	switch scheduleType {
	case "cron":
		if value == "" {
			return nil, fmt.Errorf("cron expression cannot be empty")
		}
		if _, err := cronSpecParser.Parse(value); err != nil {
			return nil, fmt.Errorf("invalid cron expression: %v", err)
		}
		schedule.Cron = value
	case "once":
		if value == "" {
			return nil, fmt.Errorf("run_at timestamp cannot be empty")
		}
		runAt, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return nil, fmt.Errorf("invalid timestamp (use RFC3339 format like '2024-12-25T09:00:00Z'): %v", err)
		}
		if time.Until(runAt) <= 0 {
			return nil, fmt.Errorf("run_at time must be in the future")
		}
		schedule.RunAt = runAt.Format(time.RFC3339)
	case "interval":
		if value == "" {
			return nil, fmt.Errorf("interval seconds cannot be empty")
		}
		seconds, err := parseSeconds(value)
		if err != nil {
			return nil, fmt.Errorf("invalid interval: %v", err)
		}
		if seconds < 10 {
			return nil, fmt.Errorf("interval must be at least 10 seconds")
		}
		schedule.IntervalSec = seconds
	default:
		return nil, fmt.Errorf("invalid schedule_type '%s' (must be 'cron', 'once', or 'interval')", scheduleType)
	}
	return schedule, nil
}

// PreviewWorkflowSchedule returns the next n run times for a schedule request.
func PreviewWorkflowSchedule(scheduleType, value string, n int) ([]string, error) {
	schedule, err := ParseWorkflowSchedule(scheduleType, value)
	if err != nil {
		return nil, err
	}
	if n <= 0 || n > 20 {
		n = 5
	}
	var out []string
	switch schedule.Type {
	case "once":
		out = append(out, schedule.RunAt)
	case "interval":
		t := time.Now()
		for i := 0; i < n; i++ {
			t = t.Add(time.Duration(schedule.IntervalSec) * time.Second)
			out = append(out, t.Format(time.RFC3339))
		}
	case "cron":
		spec, _ := cronSpecParser.Parse(schedule.Cron)
		t := time.Now()
		for i := 0; i < n; i++ {
			t = spec.Next(t)
			if t.IsZero() {
				break
			}
			out = append(out, t.Format(time.RFC3339))
		}
	}
	return out, nil
}

// ScheduleWorkflow validates, registers and persists a schedule.
func ScheduleWorkflow(id, scheduleType, value string) (*WorkflowSchedule, error) {
	ensureWorkflowsInit()
	dir, err := existingWorkflowDir(id)
	if err != nil {
		return nil, err
	}
	schedule, err := ParseWorkflowSchedule(scheduleType, value)
	if err != nil {
		return nil, err
	}
	if prev := readWorkflowSchedule(dir); prev != nil {
		schedule.LastRun = prev.LastRun
	}
	if err := registerSchedule(id, schedule); err != nil {
		return nil, err
	}
	if err := writeJSONAtomic(filepath.Join(dir, "schedule.json"), schedule); err != nil {
		unregisterSchedule(id)
		return nil, fmt.Errorf("failed to save schedule: %v", err)
	}
	return schedule, nil
}

// UnscheduleWorkflow removes a workflow's schedule (cron entry, pending
// one-time timer and schedule.json).
func UnscheduleWorkflow(id string) error {
	dir, err := existingWorkflowDir(id)
	if err != nil {
		return err
	}
	unregisterSchedule(id)
	if err := os.Remove(filepath.Join(dir, "schedule.json")); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove schedule: %v", err)
	}
	return nil
}

func unregisterScheduleLocked(id string) {
	if entryID, ok := cronEntries[id]; ok {
		getSchedulerLocked().Remove(entryID)
		delete(cronEntries, id)
	}
	if t, ok := onceTimers[id]; ok {
		t.Stop()
		delete(onceTimers, id)
	}
}

func unregisterSchedule(id string) {
	schedulerMu.Lock()
	defer schedulerMu.Unlock()
	unregisterScheduleLocked(id)
}

// registerSchedule (re)registers a schedule and fills schedule.NextRun.
func registerSchedule(id string, schedule *WorkflowSchedule) error {
	schedulerMu.Lock()
	defer schedulerMu.Unlock()
	unregisterScheduleLocked(id)

	switch schedule.Type {
	case "once":
		runAt, err := time.Parse(time.RFC3339, schedule.RunAt)
		if err != nil {
			return fmt.Errorf("invalid run_at time: %v", err)
		}
		delay := time.Until(runAt)
		if delay <= 0 {
			return fmt.Errorf("run_at time is in the past")
		}
		var timer *time.Timer
		timer = time.AfterFunc(delay, func() {
			schedulerMu.Lock()
			current, ok := onceTimers[id]
			if !ok || current != timer {
				schedulerMu.Unlock()
				return // cancelled or replaced
			}
			delete(onceTimers, id)
			schedulerMu.Unlock()
			runScheduledWorkflow(id)
			disableSchedule(id)
		})
		onceTimers[id] = timer
		schedule.NextRun = schedule.RunAt
		return nil
	case "cron", "interval":
		spec := schedule.Cron
		if schedule.Type == "interval" {
			if schedule.IntervalSec < 10 {
				return fmt.Errorf("interval must be at least 10 seconds")
			}
			spec = fmt.Sprintf("@every %ds", schedule.IntervalSec)
		}
		entryID, err := getSchedulerLocked().AddFunc(spec, func() { runScheduledWorkflow(id) })
		if err != nil {
			return fmt.Errorf("failed to add schedule: %v", err)
		}
		cronEntries[id] = entryID
		schedule.CronEntryID = int(entryID)
		if next := getSchedulerLocked().Entry(entryID).Next; !next.IsZero() {
			schedule.NextRun = next.Format(time.RFC3339)
		}
		return nil
	default:
		return fmt.Errorf("unknown schedule type: %s", schedule.Type)
	}
}

func runScheduledWorkflow(id string) {
	dir, err := existingWorkflowDir(id)
	if err != nil {
		unregisterSchedule(id)
		return
	}
	if schedule := readWorkflowSchedule(dir); schedule != nil {
		schedule.LastRun = time.Now().Format(time.RFC3339)
		schedulerMu.Lock()
		if entryID, ok := cronEntries[id]; ok {
			if next := getSchedulerLocked().Entry(entryID).Next; !next.IsZero() {
				schedule.NextRun = next.Format(time.RFC3339)
			}
		}
		schedulerMu.Unlock()
		_ = writeJSONAtomic(filepath.Join(dir, "schedule.json"), schedule)
	}
	if _, err := RunWorkflow(id, "schedule"); err != nil {
		log.Printf("[workflows] scheduled run of %s skipped: %v", id, err)
		appendWorkflowLog(dir, "Scheduled run skipped: "+err.Error())
	}
}

func disableSchedule(id string) {
	dir, err := existingWorkflowDir(id)
	if err != nil {
		return
	}
	schedule := readWorkflowSchedule(dir)
	if schedule == nil {
		return
	}
	schedule.Enabled = false
	schedule.NextRun = ""
	_ = writeJSONAtomic(filepath.Join(dir, "schedule.json"), schedule)
}

// parseSeconds parses a string as seconds (plain number or with s/m/h/d suffix).
func parseSeconds(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty value")
	}
	multiplier := 1
	switch s[len(s)-1] {
	case 's', 'S':
		s = s[:len(s)-1]
	case 'm', 'M':
		multiplier, s = 60, s[:len(s)-1]
	case 'h', 'H':
		multiplier, s = 3600, s[:len(s)-1]
	case 'd', 'D':
		multiplier, s = 86400, s[:len(s)-1]
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("invalid number %q", s)
	}
	if n < 0 {
		return 0, fmt.Errorf("value must be positive")
	}
	return n * multiplier, nil
}
