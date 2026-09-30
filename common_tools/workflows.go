package common_tools

// Agent-facing workflow tools. The exported Xxx_Workflow functions keep their
// original signatures (their JSON schemas are cached in schemas/cached_schemas)
// and run unscoped. When the chat session knows the current user it routes the
// calls through ExecuteWorkflowToolAs instead, which scopes every operation to
// that user's workflows (plus legacy/shared ones) and records ownership.
//
// All real logic lives in workflow_core.go and is shared with the REST API.

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
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

var workflowToolNames = map[string]bool{
	"Edit_Workflow": true, "Create_Workflow": true, "Run_Workflow": true,
	"Get_Workflow_Status": true, "Get_Workflow_Logs": true, "Get_Workflow_Code": true,
	"Patch_Workflow": true, "List_Workflows": true, "Stop_Workflow": true,
	"Delete_Workflow": true, "Schedule_Workflow": true, "Unschedule_Workflow": true,
}

// IsWorkflowTool reports whether name is one of the workflow agent tools.
func IsWorkflowTool(name string) bool { return workflowToolNames[name] }

// ExecuteWorkflowToolAs runs a workflow tool on behalf of actor. Returns
// handled=false if name is not a workflow tool.
func ExecuteWorkflowToolAs(actor WorkflowActor, name string, args map[string]interface{}) (result string, handled bool, err error) {
	if !IsWorkflowTool(name) {
		return "", false, nil
	}
	// Do not coerce objects or numbers into required workflow arguments.
	required := []string{"workflow_id"}
	switch name {
	case "List_Workflows":
		required = nil
	case "Create_Workflow":
		required = []string{"name", "code"}
	case "Schedule_Workflow":
		required = []string{"workflow_id", "schedule_type", "schedule_value"}
	case "Patch_Workflow":
		required = []string{"workflow_id", "find"}
	}
	for _, key := range required {
		value, ok := args[key].(string)
		if !ok || strings.TrimSpace(value) == "" {
			hint := "Provide the required argument as a nonempty string."
			if key == "workflow_id" {
				hint = `Use the exact ID returned by Create_Workflow or List_Workflows, e.g. {"workflow_id":"<returned-id>"}. This error does not establish workflow completion.`
			}
			return "", true, fmt.Errorf("%s requires a nonempty string %s. %s", name, key, hint)
		}
	}
	t := workflowTools{actor: actor}
	id := argString(args, "workflow_id")
	switch name {
	case "Edit_Workflow":
		result, err = t.edit(id, argString(args, "code"), argString(args, "name"))
	case "Create_Workflow":
		result, err = t.create(argString(args, "name"), argString(args, "code"))
	case "Run_Workflow":
		result, err = t.run(id)
	case "Get_Workflow_Status":
		result, err = t.status(id)
	case "Get_Workflow_Logs":
		result, err = t.logs(id, argInt(args, "tail_lines"))
	case "Get_Workflow_Code":
		result, err = t.code(id)
	case "Patch_Workflow":
		result, err = t.patch(id, argString(args, "find"), argString(args, "replace"), argBool(args, "replace_all"))
	case "List_Workflows":
		result, err = t.list()
	case "Stop_Workflow":
		result, err = t.stop(id)
	case "Delete_Workflow":
		result, err = t.delete(id)
	case "Schedule_Workflow":
		result, err = t.schedule(id, argString(args, "schedule_type"), argString(args, "schedule_value"))
	case "Unschedule_Workflow":
		result, err = t.unschedule(id)
	}
	return result, true, err
}

func argString(args map[string]interface{}, key string) string {
	switch v := args[key].(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

func argInt(args map[string]interface{}, key string) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	case string:
		var n int
		fmt.Sscanf(v, "%d", &n)
		return n
	}
	return 0
}

func argBool(args map[string]interface{}, key string) bool {
	switch v := args[key].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(v, "true")
	}
	return false
}

// Edit_Workflow updates the code and/or name of an existing workflow
// Use this to fix bugs, add features, or rename a workflow without deleting and recreating it
// The workflow must not be currently running
func Edit_Workflow(workflow_id string, code string, name string) (string, error) {
	return workflowTools{actor: SystemWorkflowActor}.edit(workflow_id, code, name)
}

// Create_Workflow creates a new workflow with TypeScript code that can be run later
// Returns the workflow ID and a URL that can be used to view the workflow
// The workflow code has access to the same tools as Execute_TypeScript: web, tavily, math, graph, skills
// Unlike Execute_TypeScript, workflows run in the background with a 30 minute default timeout
// Return {ok, complete, requested, processed, verified, skipped, failed, unknown} after awaiting and verifying all work
// Return {ok:false,error:"reason"} or throw on failure. A normal return alone only proves execution finished
// URLs may be relative to the current deployment
// IMPORTANT: Always present the returned URL to the user as a clickable markdown link, e.g. [View Workflow](url)
func Create_Workflow(name string, code string) (string, error) {
	return workflowTools{actor: SystemWorkflowActor}.create(name, code)
}

// Run_Workflow starts a workflow in the background
// The workflow runs as a separate process and does not block
// Use Get_Workflow_Status to check if it's still running
// Use Get_Workflow_Logs to view the execution logs
// Starting a workflow does not establish task success. Check the status, persisted result and logs
func Run_Workflow(workflow_id string) (string, error) {
	return workflowTools{actor: SystemWorkflowActor}.run(workflow_id)
}

// Get_Workflow_Status returns the current status of a workflow
// Status can be: "pending", "running", "completed", or "failed"
// Returns the persisted result and task_success when reported. completed alone does not prove business success
// If the ID is missing, use List_Workflows and select the matching returned ID
func Get_Workflow_Status(workflow_id string) (string, error) {
	return workflowTools{actor: SystemWorkflowActor}.status(workflow_id)
}

// Get_Workflow_Code returns the TypeScript source code of an existing workflow
// Use this to read a workflow's code before editing it, or to review what a workflow does
func Get_Workflow_Code(workflow_id string) (string, error) {
	return workflowTools{actor: SystemWorkflowActor}.code(workflow_id)
}

// Patch_Workflow performs a find-and-replace operation on a workflow's code
// Use this for targeted edits instead of rewriting the entire code with Edit_Workflow
// The workflow must not be currently running
// Set replace_all to true to replace all occurrences, or false to replace only the first match
func Patch_Workflow(workflow_id string, find string, replace string, replace_all bool) (string, error) {
	return workflowTools{actor: SystemWorkflowActor}.patch(workflow_id, find, replace, replace_all)
}

// Get_Workflow_Logs returns the execution logs of a workflow
// Optionally specify tail_lines to get only the last N lines (0 = all lines)
func Get_Workflow_Logs(workflow_id string, tail_lines int) (string, error) {
	return workflowTools{actor: SystemWorkflowActor}.logs(workflow_id, tail_lines)
}

// List_Workflows returns a list of all workflows and their statuses
func List_Workflows() (string, error) {
	return workflowTools{actor: SystemWorkflowActor}.list()
}

// Stop_Workflow stops a running workflow by killing its process
func Stop_Workflow(workflow_id string) (string, error) {
	return workflowTools{actor: SystemWorkflowActor}.stop(workflow_id)
}

// Delete_Workflow deletes a workflow and all its data
// A running workflow is stopped and any schedule is removed first
func Delete_Workflow(workflow_id string) (string, error) {
	return workflowTools{actor: SystemWorkflowActor}.delete(workflow_id)
}

// Schedule_Workflow schedules a workflow to run automatically
// schedule_type can be: "cron", "once", or "interval"
// - For "cron": provide a cron expression in schedule_value (e.g., "0 0 9 * * *" for 9am daily, "0 */30 * * * *" for every 30 minutes)
// - For "once": provide an ISO timestamp in schedule_value (e.g., "2024-12-25T09:00:00Z")
// - For "interval": provide seconds as schedule_value (e.g., "3600" for every hour)
// Note: Cron expressions use 6 fields (seconds minutes hours day month weekday); 5-field expressions are also accepted
func Schedule_Workflow(workflow_id string, schedule_type string, schedule_value string) (string, error) {
	return workflowTools{actor: SystemWorkflowActor}.schedule(workflow_id, schedule_type, schedule_value)
}

// Unschedule_Workflow removes the schedule from a workflow
// The workflow will no longer run automatically but can still be run manually
func Unschedule_Workflow(workflow_id string) (string, error) {
	return workflowTools{actor: SystemWorkflowActor}.unschedule(workflow_id)
}

// ---------------------------------------------------------------------------
// Actor-scoped implementations (LLM-friendly string output)
// ---------------------------------------------------------------------------

type workflowTools struct{ actor WorkflowActor }

func (t workflowTools) authorize(id string) (*WorkflowMetadata, error) {
	return t.authorizeWith(id, AuthorizeWorkflow)
}

func (t workflowTools) authorizeManage(id string) (*WorkflowMetadata, error) {
	meta, err := AuthorizeWorkflow(id, t.actor)
	if err != nil {
		return t.authorize(id)
	}
	if !t.actor.CanManage(meta) {
		return nil, fmt.Errorf("workflow '%s' is shared; only an admin can change it", id)
	}
	return meta, nil
}

func (t workflowTools) authorizeWith(id string, check func(string, WorkflowActor) (*WorkflowMetadata, error)) (*WorkflowMetadata, error) {
	if id == "" {
		return nil, fmt.Errorf("workflow_id cannot be empty. Use the exact ID returned by Create_Workflow or List_Workflows; then call Get_Workflow_Status({\"workflow_id\":\"<returned-id>\"}). This error does not establish workflow completion")
	}
	meta, err := check(id, t.actor)
	if errors.Is(err, ErrWorkflowForbidden) {
		// Do not reveal other users' workflows to the agent.
		return nil, fmt.Errorf("workflow '%s' not found", id)
	}
	return meta, err
}

func (t workflowTools) edit(id, code, name string) (string, error) {
	if _, err := t.authorizeManage(id); err != nil {
		return "", err
	}
	if code == "" && name == "" {
		return "", fmt.Errorf("at least one of 'code' or 'name' must be provided")
	}
	var p UpdateWorkflowParams
	if code != "" {
		p.Code = &code
	}
	if name != "" {
		p.Name = &name
	}
	changes, err := UpdateWorkflow(id, p)
	if err != nil {
		if errors.Is(err, ErrWorkflowRunning) {
			return "", fmt.Errorf("cannot edit workflow '%s' while it is running. Stop it first with Stop_Workflow", id)
		}
		return "", err
	}
	if len(changes) == 0 {
		return fmt.Sprintf("Workflow '%s' unchanged (new values are identical).", id), nil
	}
	return fmt.Sprintf("Workflow '%s' updated successfully. Changed: %s.\nUse Run_Workflow(\"%s\") to run the updated workflow.", id, strings.Join(changes, ", "), id), nil
}

func (t workflowTools) create(name, code string) (string, error) {
	d, err := CreateWorkflow(CreateWorkflowParams{
		Name:       name,
		Code:       code,
		OwnerUID:   t.actor.UID,
		OwnerEmail: t.actor.Email,
		CreatedBy:  "agent",
	})
	if err != nil {
		return "", err
	}
	workflowURL := fmt.Sprintf("%s/workflows/%s", getFrontendURL(), d.ID)
	return fmt.Sprintf("Workflow created successfully.\nWorkflow ID: %s\nName: %s\nURL: %s\nTimeout: %ds\n\nUse Run_Workflow(\"%s\") to start the workflow.", d.ID, d.Name, workflowURL, d.TimeoutSeconds, d.ID), nil
}

func getFrontendURL() string {
	for _, key := range []string{"FRONTEND_URL", "PUBLIC_BASE_URL"} {
		base := strings.TrimRight(strings.TrimSpace(os.Getenv(key)), "/")
		u, err := url.Parse(base)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			continue
		}
		host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
		ip := net.ParseIP(host)
		if host == "localhost" || strings.HasSuffix(host, ".localhost") || (ip != nil && (ip.IsLoopback() || ip.IsUnspecified())) {
			continue
		}
		return base
	}
	// A relative link opens on the user's current deployment, never their localhost.
	return ""
}

func (t workflowTools) run(id string) (string, error) {
	if _, err := t.authorizeManage(id); err != nil {
		return "", err
	}
	pid, err := RunWorkflow(id, "agent")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Workflow '%s' started successfully.\nPID: %d\n\nUse Get_Workflow_Status(\"%s\") to check status.\nUse Get_Workflow_Logs(\"%s\") to view logs.", id, pid, id, id), nil
}

func (t workflowTools) status(id string) (string, error) {
	if _, err := t.authorize(id); err != nil {
		return "", err
	}
	d, err := GetWorkflow(id, false)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Workflow: %s\n", id))
	if d.Name != "" {
		sb.WriteString(fmt.Sprintf("Name: %s\n", d.Name))
	}
	sb.WriteString(fmt.Sprintf("Status: %s\n", d.Status))
	if d.StartedAt != "" {
		sb.WriteString(fmt.Sprintf("Started: %s\n", d.StartedAt))
	}
	if d.CompletedAt != "" {
		sb.WriteString(fmt.Sprintf("Completed: %s\n", d.CompletedAt))
	}
	if d.PID > 0 && d.Status == "running" {
		sb.WriteString(fmt.Sprintf("PID: %d\n", d.PID))
	}
	if d.ExitCode != nil {
		sb.WriteString(fmt.Sprintf("Exit code: %d\n", *d.ExitCode))
	}
	if d.Error != "" {
		sb.WriteString(fmt.Sprintf("Error: %s\n", d.Error))
	}
	if len(d.Result) > 0 && string(d.Result) != "null" {
		sb.WriteString(fmt.Sprintf("Result: %s\n", d.Result))
	}
	if d.TaskSuccess != nil {
		sb.WriteString(fmt.Sprintf("Task success reported by script: %t\n", *d.TaskSuccess))
	} else if d.Status == "completed" {
		sb.WriteString("Execution completed; task success is unverified. Inspect Get_Workflow_Logs and verify the requested results before claiming completion.\n")
	}
	sb.WriteString(fmt.Sprintf("Timeout: %ds\n", d.TimeoutSeconds))
	return sb.String(), nil
}

func (t workflowTools) code(id string) (string, error) {
	meta, err := t.authorize(id)
	if err != nil {
		return "", err
	}
	code, err := GetWorkflowCode(id)
	if err != nil {
		return "", err
	}
	name := meta.Name
	if name == "" {
		name = "(unnamed)"
	}
	return fmt.Sprintf("Workflow: %s\nName: %s\n\n```typescript\n%s\n```", id, name, code), nil
}

func (t workflowTools) patch(id, find, replace string, replaceAll bool) (string, error) {
	if _, err := t.authorizeManage(id); err != nil {
		return "", err
	}
	n, err := PatchWorkflowCode(id, find, replace, replaceAll)
	if err != nil {
		if errors.Is(err, ErrWorkflowRunning) {
			return "", fmt.Errorf("cannot patch workflow '%s' while it is running. Stop it first with Stop_Workflow", id)
		}
		if strings.Contains(err.Error(), "find string not found") {
			return "", fmt.Errorf("find string not found in workflow code. Use Get_Workflow_Code(\"%s\") to view the current code", id)
		}
		return "", err
	}
	return fmt.Sprintf("Workflow '%s' patched successfully. Replaced %d occurrence(s).\nUse Get_Workflow_Code(\"%s\") to verify the changes.\nUse Run_Workflow(\"%s\") to run the updated workflow.", id, n, id, id), nil
}

func (t workflowTools) logs(id string, tailLines int) (string, error) {
	if _, err := t.authorize(id); err != nil {
		return "", err
	}
	logs, err := ReadWorkflowLogs(id, tailLines)
	if err != nil {
		return "", err
	}
	if logs == "" {
		return "No logs available yet. The workflow may not have started.", nil
	}
	return logs, nil
}

func (t workflowTools) list() (string, error) {
	workflows, err := ListWorkflows(t.actor)
	if err != nil {
		return "", err
	}
	if len(workflows) == 0 {
		return "No workflows found.", nil
	}
	var sb strings.Builder
	sb.WriteString("Workflows:\n\n")
	for _, w := range workflows {
		name := w.Name
		if name == "" {
			name = "(unnamed)"
		}
		line := fmt.Sprintf("- %s: %s [%s]", w.ID, name, w.Status)
		if s := w.Schedule; s != nil && s.Enabled {
			switch s.Type {
			case "cron":
				line += fmt.Sprintf(" (scheduled: %s)", s.Cron)
			case "once":
				line += fmt.Sprintf(" (run once at: %s)", s.RunAt)
			case "interval":
				line += fmt.Sprintf(" (every %ds)", s.IntervalSec)
			}
			if s.NextRun != "" {
				line += fmt.Sprintf(" [next: %s]", s.NextRun)
			}
		}
		sb.WriteString(line + "\n")
	}
	return sb.String(), nil
}

func (t workflowTools) stop(id string) (string, error) {
	if _, err := t.authorizeManage(id); err != nil {
		return "", err
	}
	if err := StopWorkflow(id); err != nil {
		return "", err
	}
	return fmt.Sprintf("Workflow '%s' stopped successfully.", id), nil
}

func (t workflowTools) delete(id string) (string, error) {
	if _, err := t.authorizeManage(id); err != nil {
		return "", err
	}
	if err := DeleteWorkflow(id); err != nil {
		return "", err
	}
	return fmt.Sprintf("Workflow '%s' deleted successfully.", id), nil
}

func (t workflowTools) schedule(id, scheduleType, value string) (string, error) {
	if _, err := t.authorizeManage(id); err != nil {
		return "", err
	}
	s, err := ScheduleWorkflow(id, scheduleType, value)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Workflow '%s' scheduled successfully.\n", id))
	sb.WriteString(fmt.Sprintf("Type: %s\n", s.Type))
	switch s.Type {
	case "cron":
		sb.WriteString(fmt.Sprintf("Cron: %s\n", s.Cron))
	case "once":
		sb.WriteString(fmt.Sprintf("Run at: %s\n", s.RunAt))
	case "interval":
		sb.WriteString(fmt.Sprintf("Interval: %d seconds\n", s.IntervalSec))
	}
	if s.NextRun != "" {
		sb.WriteString(fmt.Sprintf("Next run: %s\n", s.NextRun))
	}
	return sb.String(), nil
}

func (t workflowTools) unschedule(id string) (string, error) {
	if _, err := t.authorizeManage(id); err != nil {
		return "", err
	}
	if err := UnscheduleWorkflow(id); err != nil {
		return "", err
	}
	return fmt.Sprintf("Workflow '%s' unscheduled successfully.", id), nil
}
