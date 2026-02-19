package common_tools

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/desarso/NCA_Assistant/config"
	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
)

//go:generate ../../gen_schema -func=Edit_Workflow -file=workflows.go -out=../schemas/cached_schemas
//go:generate ../../gen_schema -func=Create_Workflow -file=workflows.go -out=../schemas/cached_schemas
//go:generate ../../gen_schema -func=Run_Workflow -file=workflows.go -out=../schemas/cached_schemas
//go:generate ../../gen_schema -func=Get_Workflow_Status -file=workflows.go -out=../schemas/cached_schemas
//go:generate ../../gen_schema -func=Get_Workflow_Logs -file=workflows.go -out=../schemas/cached_schemas
//go:generate ../../gen_schema -func=Get_Workflow_Code -file=workflows.go -out=../schemas/cached_schemas
//go:generate ../../gen_schema -func=Patch_Workflow -file=workflows.go -out=../schemas/cached_schemas
//go:generate ../../gen_schema -func=List_Workflows -file=workflows.go -out=../schemas/cached_schemas
//go:generate ../../gen_schema -func=Stop_Workflow -file=workflows.go -out=../schemas/cached_schemas
//go:generate ../../gen_schema -func=Delete_Workflow -file=workflows.go -out=../schemas/cached_schemas
//go:generate ../../gen_schema -func=Schedule_Workflow -file=workflows.go -out=../schemas/cached_schemas
//go:generate ../../gen_schema -func=Unschedule_Workflow -file=workflows.go -out=../schemas/cached_schemas

const workflowsDir = "data/workflows"

// WorkflowStatus represents the status of a workflow
type WorkflowStatus struct {
	ID          string `json:"id"`
	Status      string `json:"status"` // "pending", "running", "completed", "failed", "scheduled"
	StartedAt   string `json:"started_at,omitempty"`
	CompletedAt string `json:"completed_at,omitempty"`
	Error       string `json:"error,omitempty"`
	PID         int    `json:"pid,omitempty"`
}

// WorkflowSchedule represents scheduling configuration for a workflow
type WorkflowSchedule struct {
	Enabled     bool   `json:"enabled"`
	Type        string `json:"type"`                   // "cron", "once", "interval"
	Cron        string `json:"cron,omitempty"`         // Cron expression (e.g., "0 9 * * *" for 9am daily)
	RunAt       string `json:"run_at,omitempty"`       // ISO timestamp for one-time runs
	IntervalSec int    `json:"interval_sec,omitempty"` // Interval in seconds for repeated runs
	LastRun     string `json:"last_run,omitempty"`     // Last execution timestamp
	NextRun     string `json:"next_run,omitempty"`     // Next scheduled execution
	CronEntryID int    `json:"cron_entry_id,omitempty"`
}

// WorkflowInfo represents full workflow information
type WorkflowInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	StartedAt   string `json:"started_at,omitempty"`
	CompletedAt string `json:"completed_at,omitempty"`
	Error       string `json:"error,omitempty"`
	CreatedAt   string `json:"created_at"`
}

// Global scheduler instance
var (
	scheduler     *cron.Cron
	schedulerOnce sync.Once
	schedulerMu   sync.Mutex
	cronEntries   = make(map[string]cron.EntryID) // workflow_id -> cron entry ID
)

// getScheduler returns the global cron scheduler, initializing it if needed
func getScheduler() *cron.Cron {
	schedulerOnce.Do(func() {
		scheduler = cron.New(cron.WithSeconds())
		scheduler.Start()
		// Load existing schedules on startup
		go loadExistingSchedules()
	})
	return scheduler
}

// loadExistingSchedules loads all saved workflow schedules on startup
func loadExistingSchedules() {
	entries, err := os.ReadDir(workflowsDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		workflowID := entry.Name()
		schedulePath := filepath.Join(workflowsDir, workflowID, "schedule.json")
		scheduleBytes, err := os.ReadFile(schedulePath)
		if err != nil {
			continue
		}

		var schedule WorkflowSchedule
		if json.Unmarshal(scheduleBytes, &schedule) != nil || !schedule.Enabled {
			continue
		}

		// Re-register the schedule
		registerSchedule(workflowID, &schedule)
	}
}

// registerSchedule adds a workflow to the cron scheduler
func registerSchedule(workflowID string, schedule *WorkflowSchedule) error {
	schedulerMu.Lock()
	defer schedulerMu.Unlock()

	// Remove existing entry if any
	if entryID, exists := cronEntries[workflowID]; exists {
		getScheduler().Remove(entryID)
		delete(cronEntries, workflowID)
	}

	var entryID cron.EntryID
	var err error

	switch schedule.Type {
	case "cron":
		// Cron expression scheduling
		entryID, err = getScheduler().AddFunc(schedule.Cron, func() {
			runScheduledWorkflow(workflowID)
		})
	case "once":
		// One-time future execution
		runAt, parseErr := time.Parse(time.RFC3339, schedule.RunAt)
		if parseErr != nil {
			return fmt.Errorf("invalid run_at time: %v", parseErr)
		}
		delay := time.Until(runAt)
		if delay <= 0 {
			return fmt.Errorf("run_at time is in the past")
		}
		// Use a goroutine with timer for one-time execution
		go func() {
			timer := time.NewTimer(delay)
			<-timer.C
			runScheduledWorkflow(workflowID)
			// Disable schedule after one-time run
			disableSchedule(workflowID)
		}()
		schedule.NextRun = schedule.RunAt
		return nil
	case "interval":
		// Interval-based scheduling using cron's @every syntax
		cronExpr := fmt.Sprintf("@every %ds", schedule.IntervalSec)
		entryID, err = getScheduler().AddFunc(cronExpr, func() {
			runScheduledWorkflow(workflowID)
		})
	default:
		return fmt.Errorf("unknown schedule type: %s", schedule.Type)
	}

	if err != nil {
		return fmt.Errorf("failed to add schedule: %v", err)
	}

	cronEntries[workflowID] = entryID

	// Update next run time
	entry := getScheduler().Entry(entryID)
	if !entry.Next.IsZero() {
		schedule.NextRun = entry.Next.Format(time.RFC3339)
	}

	return nil
}

// runScheduledWorkflow executes a scheduled workflow
func runScheduledWorkflow(workflowID string) {
	// Update last run time
	schedulePath := filepath.Join(workflowsDir, workflowID, "schedule.json")
	scheduleBytes, _ := os.ReadFile(schedulePath)
	var schedule WorkflowSchedule
	json.Unmarshal(scheduleBytes, &schedule)
	schedule.LastRun = time.Now().Format(time.RFC3339)

	// Update next run time for cron/interval schedules
	schedulerMu.Lock()
	if entryID, exists := cronEntries[workflowID]; exists {
		entry := getScheduler().Entry(entryID)
		if !entry.Next.IsZero() {
			schedule.NextRun = entry.Next.Format(time.RFC3339)
		}
	}
	schedulerMu.Unlock()

	scheduleBytes, _ = json.MarshalIndent(schedule, "", "  ")
	os.WriteFile(schedulePath, scheduleBytes, 0644)

	// Run the workflow
	Run_Workflow(workflowID)
}

// disableSchedule disables a workflow's schedule (used after one-time runs)
func disableSchedule(workflowID string) {
	schedulePath := filepath.Join(workflowsDir, workflowID, "schedule.json")
	scheduleBytes, err := os.ReadFile(schedulePath)
	if err != nil {
		return
	}

	var schedule WorkflowSchedule
	if json.Unmarshal(scheduleBytes, &schedule) != nil {
		return
	}

	schedule.Enabled = false
	schedule.NextRun = ""
	scheduleBytes, _ = json.MarshalIndent(schedule, "", "  ")
	os.WriteFile(schedulePath, scheduleBytes, 0644)
}

// ensureWorkflowsDir creates the workflows directory if it doesn't exist
func ensureWorkflowsDir() error {
	return os.MkdirAll(workflowsDir, 0755)
}

// getWorkflowDir returns the directory path for a specific workflow
func getWorkflowDir(workflowID string) string {
	return filepath.Join(workflowsDir, workflowID)
}

// Edit_Workflow updates the code and/or name of an existing workflow
// Use this to fix bugs, add features, or rename a workflow without deleting and recreating it
// The workflow must not be currently running
func Edit_Workflow(workflow_id string, code string, name string) (string, error) {
	if workflow_id == "" {
		return "", fmt.Errorf("workflow_id cannot be empty")
	}

	workflowDir := getWorkflowDir(workflow_id)
	if _, err := os.Stat(workflowDir); os.IsNotExist(err) {
		return "", fmt.Errorf("workflow '%s' not found", workflow_id)
	}

	// Check workflow is not running
	statusPath := filepath.Join(workflowDir, "status.json")
	statusData, err := os.ReadFile(statusPath)
	if err == nil {
		var status WorkflowStatus
		if json.Unmarshal(statusData, &status) == nil && status.Status == "running" {
			return "", fmt.Errorf("cannot edit workflow '%s' while it is running. Stop it first with Stop_Workflow", workflow_id)
		}
	}

	if code == "" && name == "" {
		return "", fmt.Errorf("at least one of 'code' or 'name' must be provided")
	}

	var changes []string

	// Update code if provided
	if code != "" {
		codePath := filepath.Join(workflowDir, "code.ts")
		if err := os.WriteFile(codePath, []byte(code), 0644); err != nil {
			return "", fmt.Errorf("failed to update workflow code: %v", err)
		}
		changes = append(changes, "code")
	}

	// Update name if provided
	if name != "" {
		metadataPath := filepath.Join(workflowDir, "metadata.json")
		metadataData, err := os.ReadFile(metadataPath)
		if err != nil {
			return "", fmt.Errorf("failed to read workflow metadata: %v", err)
		}
		var metadata map[string]string
		if err := json.Unmarshal(metadataData, &metadata); err != nil {
			return "", fmt.Errorf("failed to parse workflow metadata: %v", err)
		}
		metadata["name"] = name
		metadataBytes, _ := json.MarshalIndent(metadata, "", "  ")
		if err := os.WriteFile(metadataPath, metadataBytes, 0644); err != nil {
			return "", fmt.Errorf("failed to update workflow metadata: %v", err)
		}
		changes = append(changes, "name")
	}

	// Reset status to pending since code may have changed
	if code != "" {
		status := WorkflowStatus{
			ID:     workflow_id,
			Status: "pending",
		}
		statusBytes, _ := json.MarshalIndent(status, "", "  ")
		if err := os.WriteFile(statusPath, statusBytes, 0644); err != nil {
			return "", fmt.Errorf("failed to reset workflow status: %v", err)
		}
	}

	return fmt.Sprintf("Workflow '%s' updated successfully. Changed: %s.\nUse Run_Workflow(\"%s\") to run the updated workflow.", workflow_id, strings.Join(changes, ", "), workflow_id), nil
}

// Create_Workflow creates a new workflow with TypeScript code that can be run later
// Returns the workflow ID and a URL that can be used to view the workflow
// The workflow code has access to the same tools as Execute_TypeScript: web, tavily, math, graph, skills
// Unlike Execute_TypeScript, workflows have no timeout and run in the background
// IMPORTANT: Always present the returned URL to the user as a clickable markdown link, e.g. [View Workflow](url)
func Create_Workflow(name string, code string) (string, error) {
	if code == "" {
		return "", fmt.Errorf("workflow code cannot be empty")
	}

	if name == "" {
		return "", fmt.Errorf("workflow name cannot be empty")
	}

	if err := ensureWorkflowsDir(); err != nil {
		return "", fmt.Errorf("failed to create workflows directory: %v", err)
	}

	// Generate unique workflow ID
	workflowID := uuid.New().String()[:8]
	workflowDir := getWorkflowDir(workflowID)

	// Create workflow directory
	if err := os.MkdirAll(workflowDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create workflow directory: %v", err)
	}

	// Save workflow code
	codePath := filepath.Join(workflowDir, "code.ts")
	if err := os.WriteFile(codePath, []byte(code), 0644); err != nil {
		return "", fmt.Errorf("failed to save workflow code: %v", err)
	}

	// Save workflow metadata
	metadata := map[string]string{
		"id":         workflowID,
		"name":       name,
		"created_at": time.Now().Format(time.RFC3339),
	}
	metadataBytes, _ := json.MarshalIndent(metadata, "", "  ")
	metadataPath := filepath.Join(workflowDir, "metadata.json")
	if err := os.WriteFile(metadataPath, metadataBytes, 0644); err != nil {
		return "", fmt.Errorf("failed to save workflow metadata: %v", err)
	}

	// Set initial status
	status := WorkflowStatus{
		ID:     workflowID,
		Status: "pending",
	}
	statusBytes, _ := json.MarshalIndent(status, "", "  ")
	statusPath := filepath.Join(workflowDir, "status.json")
	if err := os.WriteFile(statusPath, statusBytes, 0644); err != nil {
		return "", fmt.Errorf("failed to save workflow status: %v", err)
	}

	// Get frontend URL for workflow link
	frontendURL := config.GetFrontendURL()
	workflowURL := fmt.Sprintf("%s/workflows/%s", frontendURL, workflowID)

	return fmt.Sprintf("Workflow created successfully.\nWorkflow ID: %s\nName: %s\nURL: %s\n\nUse Run_Workflow(\"%s\") to start the workflow.", workflowID, name, workflowURL, workflowID), nil
}

// Run_Workflow starts a workflow in the background
// The workflow runs as a separate process and does not block
// Use Get_Workflow_Status to check if it's still running
// Use Get_Workflow_Logs to view the execution logs
func Run_Workflow(workflow_id string) (string, error) {
	if workflow_id == "" {
		return "", fmt.Errorf("workflow_id cannot be empty")
	}

	workflowDir := getWorkflowDir(workflow_id)

	// Check if workflow exists
	if _, err := os.Stat(workflowDir); os.IsNotExist(err) {
		return "", fmt.Errorf("workflow '%s' not found", workflow_id)
	}

	// Read the workflow code
	codePath := filepath.Join(workflowDir, "code.ts")
	codeBytes, err := os.ReadFile(codePath)
	if err != nil {
		return "", fmt.Errorf("failed to read workflow code: %v", err)
	}
	code := string(codeBytes)

	// Check current status
	statusPath := filepath.Join(workflowDir, "status.json")
	statusBytes, err := os.ReadFile(statusPath)
	if err == nil {
		var status WorkflowStatus
		if json.Unmarshal(statusBytes, &status) == nil {
			if status.Status == "running" {
				// Check if process is still running
				if status.PID > 0 {
					proc, err := os.FindProcess(status.PID)
					if err == nil && proc != nil {
						// On Unix, FindProcess always succeeds, so we need to check if it's actually running
						// by sending signal 0
						if proc.Signal(nil) == nil {
							return "", fmt.Errorf("workflow '%s' is already running (PID: %d)", workflow_id, status.PID)
						}
					}
				}
			}
		}
	}

	// Find Bun executable
	bunPath, err := findBun()
	if err != nil {
		return "", err
	}

	// Clear previous logs
	logsPath := filepath.Join(workflowDir, "logs.txt")
	os.Remove(logsPath)

	// Get the path to the workflow executor
	executorPath := "helpers/typescript_runtime/workflow_executor.ts"

	// Start the workflow as a background process
	cmd := exec.Command(bunPath, executorPath, workflow_id, code)
	cmd.Env = os.Environ()

	// Redirect stdout/stderr to files for debugging (the executor writes its own logs)
	stdoutFile, _ := os.Create(filepath.Join(workflowDir, "stdout.txt"))
	stderrFile, _ := os.Create(filepath.Join(workflowDir, "stderr.txt"))
	cmd.Stdout = stdoutFile
	cmd.Stderr = stderrFile

	// Start the process (non-blocking)
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("failed to start workflow: %v", err)
	}

	// Detach the process so it continues running after we return
	go func() {
		cmd.Wait()
		stdoutFile.Close()
		stderrFile.Close()
	}()

	return fmt.Sprintf("Workflow '%s' started successfully.\nPID: %d\n\nUse Get_Workflow_Status(\"%s\") to check status.\nUse Get_Workflow_Logs(\"%s\") to view logs.", workflow_id, cmd.Process.Pid, workflow_id, workflow_id), nil
}

// Get_Workflow_Status returns the current status of a workflow
// Status can be: "pending", "running", "completed", or "failed"
func Get_Workflow_Status(workflow_id string) (string, error) {
	if workflow_id == "" {
		return "", fmt.Errorf("workflow_id cannot be empty")
	}

	workflowDir := getWorkflowDir(workflow_id)

	// Check if workflow exists
	if _, err := os.Stat(workflowDir); os.IsNotExist(err) {
		return "", fmt.Errorf("workflow '%s' not found", workflow_id)
	}

	// Read status
	statusPath := filepath.Join(workflowDir, "status.json")
	statusBytes, err := os.ReadFile(statusPath)
	if err != nil {
		return "", fmt.Errorf("failed to read workflow status: %v", err)
	}

	var status WorkflowStatus
	if err := json.Unmarshal(statusBytes, &status); err != nil {
		return "", fmt.Errorf("failed to parse workflow status: %v", err)
	}

	// Read metadata for name
	metadataPath := filepath.Join(workflowDir, "metadata.json")
	metadataBytes, _ := os.ReadFile(metadataPath)
	var metadata map[string]string
	json.Unmarshal(metadataBytes, &metadata)

	// Build response
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Workflow: %s\n", workflow_id))
	if name, ok := metadata["name"]; ok {
		sb.WriteString(fmt.Sprintf("Name: %s\n", name))
	}
	sb.WriteString(fmt.Sprintf("Status: %s\n", status.Status))
	if status.StartedAt != "" {
		sb.WriteString(fmt.Sprintf("Started: %s\n", status.StartedAt))
	}
	if status.CompletedAt != "" {
		sb.WriteString(fmt.Sprintf("Completed: %s\n", status.CompletedAt))
	}
	if status.PID > 0 {
		sb.WriteString(fmt.Sprintf("PID: %d\n", status.PID))
	}
	if status.Error != "" {
		sb.WriteString(fmt.Sprintf("Error: %s\n", status.Error))
	}

	return sb.String(), nil
}

// Get_Workflow_Code returns the TypeScript source code of an existing workflow
// Use this to read a workflow's code before editing it, or to review what a workflow does
func Get_Workflow_Code(workflow_id string) (string, error) {
	if workflow_id == "" {
		return "", fmt.Errorf("workflow_id cannot be empty")
	}

	workflowDir := getWorkflowDir(workflow_id)

	// Check if workflow exists
	if _, err := os.Stat(workflowDir); os.IsNotExist(err) {
		return "", fmt.Errorf("workflow '%s' not found", workflow_id)
	}

	// Read the code
	codePath := filepath.Join(workflowDir, "code.ts")
	codeBytes, err := os.ReadFile(codePath)
	if err != nil {
		return "", fmt.Errorf("failed to read workflow code: %v", err)
	}

	// Read metadata for name
	metadataPath := filepath.Join(workflowDir, "metadata.json")
	metadataBytes, _ := os.ReadFile(metadataPath)
	var metadata map[string]string
	json.Unmarshal(metadataBytes, &metadata)

	name := metadata["name"]
	if name == "" {
		name = "(unnamed)"
	}

	return fmt.Sprintf("Workflow: %s\nName: %s\n\n```typescript\n%s\n```", workflow_id, name, string(codeBytes)), nil
}

// Patch_Workflow performs a find-and-replace operation on a workflow's code
// Use this for targeted edits instead of rewriting the entire code with Edit_Workflow
// The workflow must not be currently running
// Set replace_all to true to replace all occurrences, or false to replace only the first match
func Patch_Workflow(workflow_id string, find string, replace string, replace_all bool) (string, error) {
	if workflow_id == "" {
		return "", fmt.Errorf("workflow_id cannot be empty")
	}
	if find == "" {
		return "", fmt.Errorf("find string cannot be empty")
	}

	workflowDir := getWorkflowDir(workflow_id)
	if _, err := os.Stat(workflowDir); os.IsNotExist(err) {
		return "", fmt.Errorf("workflow '%s' not found", workflow_id)
	}

	// Check workflow is not running
	statusPath := filepath.Join(workflowDir, "status.json")
	statusData, err := os.ReadFile(statusPath)
	if err == nil {
		var status WorkflowStatus
		if json.Unmarshal(statusData, &status) == nil && status.Status == "running" {
			return "", fmt.Errorf("cannot patch workflow '%s' while it is running. Stop it first with Stop_Workflow", workflow_id)
		}
	}

	// Read current code
	codePath := filepath.Join(workflowDir, "code.ts")
	codeBytes, err := os.ReadFile(codePath)
	if err != nil {
		return "", fmt.Errorf("failed to read workflow code: %v", err)
	}
	code := string(codeBytes)

	// Check that the find string exists in the code
	count := strings.Count(code, find)
	if count == 0 {
		return "", fmt.Errorf("find string not found in workflow code. Use Get_Workflow_Code(\"%s\") to view the current code", workflow_id)
	}

	// Perform replacement
	var newCode string
	if replace_all {
		newCode = strings.ReplaceAll(code, find, replace)
	} else {
		newCode = strings.Replace(code, find, replace, 1)
	}

	// Write updated code
	if err := os.WriteFile(codePath, []byte(newCode), 0644); err != nil {
		return "", fmt.Errorf("failed to write updated workflow code: %v", err)
	}

	// Reset status to pending
	newStatus := WorkflowStatus{
		ID:     workflow_id,
		Status: "pending",
	}
	statusBytes, _ := json.MarshalIndent(newStatus, "", "  ")
	os.WriteFile(statusPath, statusBytes, 0644)

	replacedCount := count
	if !replace_all {
		replacedCount = 1
	}

	return fmt.Sprintf("Workflow '%s' patched successfully. Replaced %d occurrence(s).\nUse Get_Workflow_Code(\"%s\") to verify the changes.\nUse Run_Workflow(\"%s\") to run the updated workflow.", workflow_id, replacedCount, workflow_id, workflow_id), nil
}

// Get_Workflow_Logs returns the execution logs of a workflow
// Optionally specify tail_lines to get only the last N lines (0 = all lines)
func Get_Workflow_Logs(workflow_id string, tail_lines int) (string, error) {
	if workflow_id == "" {
		return "", fmt.Errorf("workflow_id cannot be empty")
	}

	workflowDir := getWorkflowDir(workflow_id)

	// Check if workflow exists
	if _, err := os.Stat(workflowDir); os.IsNotExist(err) {
		return "", fmt.Errorf("workflow '%s' not found", workflow_id)
	}

	// Read logs
	logsPath := filepath.Join(workflowDir, "logs.txt")
	logsBytes, err := os.ReadFile(logsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "No logs available yet. The workflow may not have started.", nil
		}
		return "", fmt.Errorf("failed to read workflow logs: %v", err)
	}

	logs := string(logsBytes)

	// Apply tail if specified
	if tail_lines > 0 {
		lines := strings.Split(logs, "\n")
		if len(lines) > tail_lines {
			lines = lines[len(lines)-tail_lines:]
		}
		logs = strings.Join(lines, "\n")
	}

	if logs == "" {
		return "Logs are empty.", nil
	}

	return logs, nil
}

// List_Workflows returns a list of all workflows and their statuses
func List_Workflows() (string, error) {
	if err := ensureWorkflowsDir(); err != nil {
		return "", fmt.Errorf("failed to access workflows directory: %v", err)
	}

	entries, err := os.ReadDir(workflowsDir)
	if err != nil {
		return "", fmt.Errorf("failed to list workflows: %v", err)
	}

	if len(entries) == 0 {
		return "No workflows found.", nil
	}

	var sb strings.Builder
	sb.WriteString("Workflows:\n\n")

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		workflowID := entry.Name()
		workflowDir := getWorkflowDir(workflowID)

		// Read metadata
		metadataPath := filepath.Join(workflowDir, "metadata.json")
		metadataBytes, _ := os.ReadFile(metadataPath)
		var metadata map[string]string
		json.Unmarshal(metadataBytes, &metadata)

		// Read status
		statusPath := filepath.Join(workflowDir, "status.json")
		statusBytes, _ := os.ReadFile(statusPath)
		var status WorkflowStatus
		json.Unmarshal(statusBytes, &status)

		// Read schedule
		schedulePath := filepath.Join(workflowDir, "schedule.json")
		scheduleBytes, _ := os.ReadFile(schedulePath)
		var schedule WorkflowSchedule
		json.Unmarshal(scheduleBytes, &schedule)

		name := metadata["name"]
		if name == "" {
			name = "(unnamed)"
		}

		// Build workflow line
		line := fmt.Sprintf("- %s: %s [%s]", workflowID, name, status.Status)
		if schedule.Enabled {
			switch schedule.Type {
			case "cron":
				line += fmt.Sprintf(" (scheduled: %s)", schedule.Cron)
			case "once":
				line += fmt.Sprintf(" (run once at: %s)", schedule.RunAt)
			case "interval":
				line += fmt.Sprintf(" (every %ds)", schedule.IntervalSec)
			}
			if schedule.NextRun != "" {
				line += fmt.Sprintf(" [next: %s]", schedule.NextRun)
			}
		}
		sb.WriteString(line + "\n")
	}

	return sb.String(), nil
}

// Stop_Workflow stops a running workflow by killing its process
func Stop_Workflow(workflow_id string) (string, error) {
	if workflow_id == "" {
		return "", fmt.Errorf("workflow_id cannot be empty")
	}

	workflowDir := getWorkflowDir(workflow_id)

	// Check if workflow exists
	if _, err := os.Stat(workflowDir); os.IsNotExist(err) {
		return "", fmt.Errorf("workflow '%s' not found", workflow_id)
	}

	// Read status
	statusPath := filepath.Join(workflowDir, "status.json")
	statusBytes, err := os.ReadFile(statusPath)
	if err != nil {
		return "", fmt.Errorf("failed to read workflow status: %v", err)
	}

	var status WorkflowStatus
	if err := json.Unmarshal(statusBytes, &status); err != nil {
		return "", fmt.Errorf("failed to parse workflow status: %v", err)
	}

	if status.Status != "running" {
		return "", fmt.Errorf("workflow '%s' is not running (status: %s)", workflow_id, status.Status)
	}

	if status.PID <= 0 {
		return "", fmt.Errorf("workflow '%s' has no valid PID", workflow_id)
	}

	// Find and kill the process
	proc, err := os.FindProcess(status.PID)
	if err != nil {
		return "", fmt.Errorf("failed to find process %d: %v", status.PID, err)
	}

	if err := proc.Kill(); err != nil {
		return "", fmt.Errorf("failed to kill process %d: %v", status.PID, err)
	}

	// Update status
	status.Status = "failed"
	status.CompletedAt = time.Now().Format(time.RFC3339)
	status.Error = "Stopped by user"
	statusBytes, _ = json.MarshalIndent(status, "", "  ")
	os.WriteFile(statusPath, statusBytes, 0644)

	// Append to logs
	logsPath := filepath.Join(workflowDir, "logs.txt")
	f, _ := os.OpenFile(logsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if f != nil {
		f.WriteString(fmt.Sprintf("[%s] Workflow stopped by user\n", time.Now().Format(time.RFC3339)))
		f.Close()
	}

	return fmt.Sprintf("Workflow '%s' stopped successfully.", workflow_id), nil
}

// Delete_Workflow deletes a workflow and all its data
// Cannot delete a running workflow - stop it first
func Delete_Workflow(workflow_id string) (string, error) {
	if workflow_id == "" {
		return "", fmt.Errorf("workflow_id cannot be empty")
	}

	workflowDir := getWorkflowDir(workflow_id)

	// Check if workflow exists
	if _, err := os.Stat(workflowDir); os.IsNotExist(err) {
		return "", fmt.Errorf("workflow '%s' not found", workflow_id)
	}

	// Read status to check if running
	statusPath := filepath.Join(workflowDir, "status.json")
	statusBytes, _ := os.ReadFile(statusPath)
	var status WorkflowStatus
	if json.Unmarshal(statusBytes, &status) == nil {
		if status.Status == "running" {
			// Check if process is still running
			if status.PID > 0 {
				proc, err := os.FindProcess(status.PID)
				if err == nil && proc != nil {
					if proc.Signal(nil) == nil {
						return "", fmt.Errorf("cannot delete running workflow '%s' - stop it first", workflow_id)
					}
				}
			}
		}
	}

	// Delete the workflow directory
	if err := os.RemoveAll(workflowDir); err != nil {
		return "", fmt.Errorf("failed to delete workflow: %v", err)
	}

	return fmt.Sprintf("Workflow '%s' deleted successfully.", workflow_id), nil
}

// Schedule_Workflow schedules a workflow to run automatically
// schedule_type can be: "cron", "once", or "interval"
// - For "cron": provide a cron expression in schedule_value (e.g., "0 0 9 * * *" for 9am daily, "0 */30 * * * *" for every 30 minutes)
// - For "once": provide an ISO timestamp in schedule_value (e.g., "2024-12-25T09:00:00Z")
// - For "interval": provide seconds as schedule_value (e.g., "3600" for every hour)
// Note: Cron expressions use 6 fields (seconds minutes hours day month weekday)
func Schedule_Workflow(workflow_id string, schedule_type string, schedule_value string) (string, error) {
	if workflow_id == "" {
		return "", fmt.Errorf("workflow_id cannot be empty")
	}

	workflowDir := getWorkflowDir(workflow_id)

	// Check if workflow exists
	if _, err := os.Stat(workflowDir); os.IsNotExist(err) {
		return "", fmt.Errorf("workflow '%s' not found", workflow_id)
	}

	// Validate schedule type and value
	schedule := WorkflowSchedule{
		Enabled: true,
		Type:    schedule_type,
	}

	switch schedule_type {
	case "cron":
		if schedule_value == "" {
			return "", fmt.Errorf("cron expression cannot be empty")
		}
		// Validate cron expression
		_, err := cron.ParseStandard(schedule_value)
		if err != nil {
			// Try with seconds
			parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
			_, err = parser.Parse(schedule_value)
			if err != nil {
				return "", fmt.Errorf("invalid cron expression: %v", err)
			}
		}
		schedule.Cron = schedule_value

	case "once":
		if schedule_value == "" {
			return "", fmt.Errorf("run_at timestamp cannot be empty")
		}
		// Validate timestamp
		runAt, err := time.Parse(time.RFC3339, schedule_value)
		if err != nil {
			return "", fmt.Errorf("invalid timestamp (use RFC3339 format like '2024-12-25T09:00:00Z'): %v", err)
		}
		if time.Until(runAt) <= 0 {
			return "", fmt.Errorf("run_at time must be in the future")
		}
		schedule.RunAt = schedule_value

	case "interval":
		if schedule_value == "" {
			return "", fmt.Errorf("interval seconds cannot be empty")
		}
		seconds, err := parseSeconds(schedule_value)
		if err != nil {
			return "", fmt.Errorf("invalid interval: %v", err)
		}
		if seconds < 10 {
			return "", fmt.Errorf("interval must be at least 10 seconds")
		}
		schedule.IntervalSec = seconds

	default:
		return "", fmt.Errorf("invalid schedule_type '%s' (must be 'cron', 'once', or 'interval')", schedule_type)
	}

	// Register the schedule
	if err := registerSchedule(workflow_id, &schedule); err != nil {
		return "", err
	}

	// Save schedule to file
	schedulePath := filepath.Join(workflowDir, "schedule.json")
	scheduleBytes, _ := json.MarshalIndent(schedule, "", "  ")
	if err := os.WriteFile(schedulePath, scheduleBytes, 0644); err != nil {
		return "", fmt.Errorf("failed to save schedule: %v", err)
	}

	// Build response
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Workflow '%s' scheduled successfully.\n", workflow_id))
	sb.WriteString(fmt.Sprintf("Type: %s\n", schedule_type))
	switch schedule_type {
	case "cron":
		sb.WriteString(fmt.Sprintf("Cron: %s\n", schedule.Cron))
	case "once":
		sb.WriteString(fmt.Sprintf("Run at: %s\n", schedule.RunAt))
	case "interval":
		sb.WriteString(fmt.Sprintf("Interval: %d seconds\n", schedule.IntervalSec))
	}
	if schedule.NextRun != "" {
		sb.WriteString(fmt.Sprintf("Next run: %s\n", schedule.NextRun))
	}

	return sb.String(), nil
}

// parseSeconds parses a string as seconds (can be just a number or with units like "30s", "5m", "1h")
func parseSeconds(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty value")
	}

	// Check for unit suffix
	lastChar := s[len(s)-1]
	switch lastChar {
	case 's', 'S':
		// Seconds
		val := s[:len(s)-1]
		var seconds int
		_, err := fmt.Sscanf(val, "%d", &seconds)
		return seconds, err
	case 'm', 'M':
		// Minutes
		val := s[:len(s)-1]
		var minutes int
		_, err := fmt.Sscanf(val, "%d", &minutes)
		return minutes * 60, err
	case 'h', 'H':
		// Hours
		val := s[:len(s)-1]
		var hours int
		_, err := fmt.Sscanf(val, "%d", &hours)
		return hours * 3600, err
	case 'd', 'D':
		// Days
		val := s[:len(s)-1]
		var days int
		_, err := fmt.Sscanf(val, "%d", &days)
		return days * 86400, err
	default:
		// Just a number (assume seconds)
		var seconds int
		_, err := fmt.Sscanf(s, "%d", &seconds)
		return seconds, err
	}
}

// Unschedule_Workflow removes the schedule from a workflow
// The workflow will no longer run automatically but can still be run manually
func Unschedule_Workflow(workflow_id string) (string, error) {
	if workflow_id == "" {
		return "", fmt.Errorf("workflow_id cannot be empty")
	}

	workflowDir := getWorkflowDir(workflow_id)

	// Check if workflow exists
	if _, err := os.Stat(workflowDir); os.IsNotExist(err) {
		return "", fmt.Errorf("workflow '%s' not found", workflow_id)
	}

	// Remove from cron scheduler
	schedulerMu.Lock()
	if entryID, exists := cronEntries[workflow_id]; exists {
		getScheduler().Remove(entryID)
		delete(cronEntries, workflow_id)
	}
	schedulerMu.Unlock()

	// Remove schedule file
	schedulePath := filepath.Join(workflowDir, "schedule.json")
	os.Remove(schedulePath)

	return fmt.Sprintf("Workflow '%s' unscheduled successfully.", workflow_id), nil
}
