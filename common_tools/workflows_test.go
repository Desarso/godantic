package common_tools

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// useTempWorkflowsDir points the workflow store at a temp dir for one test.
func useTempWorkflowsDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	prev := workflowsDir
	workflowsDir = filepath.Join(dir, "workflows")
	t.Cleanup(func() { workflowsDir = prev })
	return workflowsDir
}

func TestWorkflowIDValidation(t *testing.T) {
	valid := []string{"a1b2c3d4", "my-workflow", "under_score", strings.Repeat("a", 64)}
	for _, id := range valid {
		if err := ValidateWorkflowID(id); err != nil {
			t.Errorf("expected %q to be valid, got %v", id, err)
		}
	}
	invalid := []string{"", "..", "../etc", "a/b", `a\b`, "a b", "a.b", strings.Repeat("a", 65), "id\x00", "ü"}
	for _, id := range invalid {
		if err := ValidateWorkflowID(id); !errors.Is(err, ErrInvalidWorkflowID) {
			t.Errorf("expected %q to be rejected, got %v", id, err)
		}
	}
}

func TestWorkflowPathTraversalRejectedEverywhere(t *testing.T) {
	root := useTempWorkflowsDir(t)
	// A sibling directory that traversal would reach.
	secret := filepath.Join(filepath.Dir(root), "secret")
	if err := os.MkdirAll(secret, 0755); err != nil {
		t.Fatal(err)
	}
	id := "../secret"
	if _, err := GetWorkflow(id, true); !errors.Is(err, ErrInvalidWorkflowID) {
		t.Errorf("GetWorkflow: %v", err)
	}
	if err := DeleteWorkflow(id); !errors.Is(err, ErrInvalidWorkflowID) {
		t.Errorf("DeleteWorkflow: %v", err)
	}
	if _, err := RunWorkflow(id, "manual"); !errors.Is(err, ErrInvalidWorkflowID) {
		t.Errorf("RunWorkflow: %v", err)
	}
	if _, err := Delete_Workflow(id); err == nil {
		t.Errorf("Delete_Workflow tool accepted traversal id")
	}
	if _, err := os.Stat(secret); err != nil {
		t.Fatalf("secret dir was touched: %v", err)
	}
}

func TestWorkflowEnvAllowlist(t *testing.T) {
	fake := map[string]string{
		"PATH":                "/usr/bin",
		"HOME":                "/home/x",
		"TZ":                  "UTC",
		"TS_RUNTIME_TOOLS":    "web,tavily,graph,skills",
		"TAVILY_API_KEY":      "tv",
		"MS_TENANT_ID":        "tenant",
		"MS_APP_ID":           "app",
		"MS_SECRET":           "secret",
		"CLIENT_ID":           "whagons",
		"DATABASE_URL":        "postgres://secret",
		"OPENAI_API_KEY":      "sk-openai",
		"OPENROUTER_API_KEY":  "sk-or",
		"FIREBASE_CONFIG":     "{}",
		"GEMINI_API_KEY":      "gem",
		"AGENT_SANDBOX_TOKEN": "sandbox",
	}
	env := BuildWorkflowEnv(func(k string) string { return fake[k] }, map[string]string{
		"WORKFLOW_ID":             "abc",
		"AGENT_USER_WORKSPACE_ID": "uid-1",
		"EMPTY":                   "",
	})
	got := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		got[k] = v
	}
	for _, k := range []string{"PATH", "HOME", "TZ", "TS_RUNTIME_TOOLS", "TAVILY_API_KEY", "MS_TENANT_ID", "MS_APP_ID", "MS_SECRET", "CLIENT_ID", "WORKFLOW_ID", "AGENT_USER_WORKSPACE_ID"} {
		if got[k] != fake[k] && k != "WORKFLOW_ID" && k != "AGENT_USER_WORKSPACE_ID" {
			t.Errorf("expected %s to be forwarded", k)
		}
		if _, ok := got[k]; !ok {
			t.Errorf("missing %s", k)
		}
	}
	// Tools not enabled (workspace, image, sandbox) must not leak their secrets.
	for _, k := range []string{"DATABASE_URL", "OPENAI_API_KEY", "OPENROUTER_API_KEY", "FIREBASE_CONFIG", "GEMINI_API_KEY", "AGENT_SANDBOX_TOKEN", "EMPTY"} {
		if _, ok := got[k]; ok {
			t.Errorf("%s must not be forwarded", k)
		}
	}
	if got["AGENT_GLOBAL_WORKSPACE_ID"] != "whagons" {
		t.Errorf("AGENT_GLOBAL_WORKSPACE_ID = %q, want CLIENT_ID fallback", got["AGENT_GLOBAL_WORKSPACE_ID"])
	}

	// Default tool set when TS_RUNTIME_TOOLS is unset: no graph secrets.
	env = BuildWorkflowEnv(func(k string) string {
		if k == "TS_RUNTIME_TOOLS" {
			return ""
		}
		return fake[k]
	}, nil)
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, "TAVILY_API_KEY=tv") || strings.Contains(joined, "MS_SECRET") {
		t.Errorf("unexpected default env: %v", env)
	}
}

func TestWorkflowActorAccess(t *testing.T) {
	owned := &WorkflowMetadata{OwnerUID: "u1", OwnerEmail: "a@x.com"}
	legacy := &WorkflowMetadata{}
	if !(WorkflowActor{UID: "u1"}).CanAccess(owned) {
		t.Error("owner by uid denied")
	}
	if !(WorkflowActor{UID: "other", Email: "A@X.com"}).CanAccess(owned) {
		t.Error("owner by email denied")
	}
	if (WorkflowActor{UID: "u2", Email: "b@x.com"}).CanAccess(owned) {
		t.Error("stranger allowed")
	}
	if !(WorkflowActor{UID: "u2", IsAdmin: true}).CanAccess(owned) {
		t.Error("admin denied")
	}
	if !(WorkflowActor{UID: "u2"}).CanAccess(legacy) {
		t.Error("legacy workflow should be shared")
	}
}

// fakeRunner replaces pnpm/tsx with a shell that spawns a child and sleeps,
// so process-group handling can be tested without node.
func useFakeRunner(t *testing.T, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("process groups are unix-only")
	}
	prev := findWorkflowRunner
	findWorkflowRunner = func() (typescriptRunner, error) {
		return typescriptRunner{command: "sh", args: []string{"-c", script, "sh"}, dir: t.TempDir()}, nil
	}
	t.Cleanup(func() { findWorkflowRunner = prev })
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond()
}

func TestWorkflowRunningStateTracking(t *testing.T) {
	useTempWorkflowsDir(t)
	// Parent shell with a background child: both must die on stop.
	useFakeRunner(t, "sleep 30 & sleep 30; wait")

	d, err := CreateWorkflow(CreateWorkflowParams{Name: "t", Code: "console.log(1)", OwnerUID: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	if IsWorkflowRunning(d.ID) {
		t.Fatal("should not be running before start")
	}
	pid, err := RunWorkflow(d.ID, "manual")
	if err != nil {
		t.Fatal(err)
	}
	if !IsWorkflowRunning(d.ID) {
		t.Fatal("should be running after start")
	}
	if _, err := RunWorkflow(d.ID, "manual"); !errors.Is(err, ErrWorkflowRunning) {
		t.Fatalf("second run: expected ErrWorkflowRunning, got %v", err)
	}
	code := "x"
	if _, err := UpdateWorkflow(d.ID, UpdateWorkflowParams{Code: &code}); !errors.Is(err, ErrWorkflowRunning) {
		t.Fatalf("update while running: expected ErrWorkflowRunning, got %v", err)
	}
	got, _ := GetWorkflow(d.ID, false)
	if got.Status != "running" {
		t.Fatalf("status = %s, want running", got.Status)
	}

	if err := StopWorkflow(d.ID); err != nil {
		t.Fatal(err)
	}
	if IsWorkflowRunning(d.ID) {
		t.Fatal("still tracked as running after stop")
	}
	if !waitFor(t, 2*time.Second, func() bool { return !workflowProcessAlive(-pid) }) {
		t.Fatal("process group still alive after stop")
	}
	got, _ = GetWorkflow(d.ID, false)
	if got.Status != "failed" || got.Error != "Stopped by user" {
		t.Fatalf("after stop: status=%s error=%q", got.Status, got.Error)
	}
	if err := StopWorkflow(d.ID); !errors.Is(err, ErrWorkflowNotRunning) {
		t.Fatalf("stop when idle: expected ErrWorkflowNotRunning, got %v", err)
	}
}

func TestWorkflowTimeoutKillsProcess(t *testing.T) {
	useTempWorkflowsDir(t)
	useFakeRunner(t, "sleep 30")
	workflowTimeoutOverride = 300 * time.Millisecond
	t.Cleanup(func() { workflowTimeoutOverride = 0 })

	d, err := CreateWorkflow(CreateWorkflowParams{Name: "t", Code: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RunWorkflow(d.ID, "manual"); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, 3*time.Second, func() bool { return !IsWorkflowRunning(d.ID) }) {
		t.Fatal("workflow not killed by timeout")
	}
	got, _ := GetWorkflow(d.ID, false)
	if got.Status != "failed" || !strings.HasPrefix(got.Error, "Timed out") {
		t.Fatalf("status=%s error=%q", got.Status, got.Error)
	}
}

func TestWorkflowUnexpectedExitMarkedFailed(t *testing.T) {
	useTempWorkflowsDir(t)
	useFakeRunner(t, "echo boom >&2; exit 3")
	d, _ := CreateWorkflow(CreateWorkflowParams{Name: "t", Code: "1"})
	if _, err := RunWorkflow(d.ID, "manual"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 3*time.Second, func() bool { return !IsWorkflowRunning(d.ID) })
	got, _ := GetWorkflow(d.ID, false)
	if got.Status != "failed" || got.ExitCode == nil || *got.ExitCode != 3 || !strings.Contains(got.Error, "boom") {
		t.Fatalf("status=%s exit=%v error=%q", got.Status, got.ExitCode, got.Error)
	}
}

func TestWorkflowScheduleCancelAndDelete(t *testing.T) {
	useTempWorkflowsDir(t)
	d, _ := CreateWorkflow(CreateWorkflowParams{Name: "t", Code: "1"})

	runAt := time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339)
	if _, err := ScheduleWorkflow(d.ID, "once", runAt); err != nil {
		t.Fatal(err)
	}
	schedulerMu.Lock()
	_, hasTimer := onceTimers[d.ID]
	schedulerMu.Unlock()
	if !hasTimer {
		t.Fatal("once timer not registered")
	}
	if err := UnscheduleWorkflow(d.ID); err != nil {
		t.Fatal(err)
	}
	schedulerMu.Lock()
	_, hasTimer = onceTimers[d.ID]
	schedulerMu.Unlock()
	if hasTimer {
		t.Fatal("once timer not cancelled")
	}

	// 5-field and 6-field cron are both accepted.
	if _, err := ScheduleWorkflow(d.ID, "cron", "0 9 * * *"); err != nil {
		t.Fatalf("5-field cron: %v", err)
	}
	s, err := ScheduleWorkflow(d.ID, "cron", "0 0 9 * * *")
	if err != nil || s.NextRun == "" {
		t.Fatalf("6-field cron: %v next=%q", err, s.NextRun)
	}
	schedulerMu.Lock()
	_, hasEntry := cronEntries[d.ID]
	schedulerMu.Unlock()
	if !hasEntry {
		t.Fatal("cron entry missing")
	}
	if err := DeleteWorkflow(d.ID); err != nil {
		t.Fatal(err)
	}
	schedulerMu.Lock()
	_, hasEntry = cronEntries[d.ID]
	schedulerMu.Unlock()
	if hasEntry {
		t.Fatal("cron entry not removed on delete")
	}

	if _, err := ParseWorkflowSchedule("interval", "5"); err == nil {
		t.Error("interval < 10s accepted")
	}
	if s, err := ParseWorkflowSchedule("interval", "5m"); err != nil || s.IntervalSec != 300 {
		t.Errorf("interval 5m: %v %+v", err, s)
	}
}

func TestWorkflowToolScopingHidesOtherUsers(t *testing.T) {
	useTempWorkflowsDir(t)
	res, handled, err := ExecuteWorkflowToolAs(WorkflowActor{UID: "alice"}, "Create_Workflow", map[string]interface{}{"name": "a", "code": "1"})
	if !handled || err != nil {
		t.Fatal(handled, err)
	}
	id := ""
	for _, line := range strings.Split(res, "\n") {
		if strings.HasPrefix(line, "Workflow ID: ") {
			id = strings.TrimPrefix(line, "Workflow ID: ")
		}
	}
	meta, err := LoadWorkflowMetadata(id)
	if err != nil || meta.OwnerUID != "alice" || meta.CreatedBy != "agent" {
		t.Fatalf("owner not recorded: %+v %v", meta, err)
	}
	if _, _, err := ExecuteWorkflowToolAs(WorkflowActor{UID: "bob"}, "Get_Workflow_Code", map[string]interface{}{"workflow_id": id}); err == nil {
		t.Fatal("bob could read alice's workflow")
	}
	list, _, _ := ExecuteWorkflowToolAs(WorkflowActor{UID: "bob"}, "List_Workflows", nil)
	if strings.Contains(list, id) {
		t.Fatal("bob can list alice's workflow")
	}
	if _, handled, _ := ExecuteWorkflowToolAs(WorkflowActor{}, "Execute_TypeScript", nil); handled {
		t.Fatal("non-workflow tool handled")
	}
}

func TestWorkflowCanManageLegacyIsAdminOnly(t *testing.T) {
	legacy := &WorkflowMetadata{}
	owned := &WorkflowMetadata{OwnerUID: "u1"}
	if (WorkflowActor{UID: "u2"}).CanManage(legacy) {
		t.Fatal("non-admins must not change ownerless workflows")
	}
	if !(WorkflowActor{UID: "u2", IsAdmin: true}).CanManage(legacy) {
		t.Fatal("admins can change ownerless workflows")
	}
	if !(WorkflowActor{UID: "u1"}).CanManage(owned) {
		t.Fatal("owners can change their workflows")
	}
	if (WorkflowActor{UID: "u2"}).CanManage(owned) {
		t.Fatal("other users must not change someone else's workflow")
	}
}

func TestWorkflowPublicURL(t *testing.T) {
	for _, tc := range []struct{ frontend, public, want string }{
		{"", "", ""},
		{"https://assistant.example.com/", "https://other.example.com", "https://assistant.example.com"},
		{"", "https://public.example.com/app/", "https://public.example.com/app"},
		{"http://localhost:3000", "", ""},
		{"http://127.0.0.1:3000", "https://public.example.com", "https://public.example.com"},
		{"http://[::1]:3000", "", ""},
		{"http://0.0.0.0:3000", "", ""},
		{"http://LOCALHOST.:3000", "", ""},
		{"javascript:alert(1)", "", ""},
		{"https://user:pass@example.com", "", ""},
		{"https://example.com?x=y", "", ""},
	} {
		t.Run(tc.frontend+tc.public, func(t *testing.T) {
			t.Setenv("FRONTEND_URL", tc.frontend)
			t.Setenv("PUBLIC_BASE_URL", tc.public)
			if got := getFrontendURL(); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
	useTempWorkflowsDir(t)
	t.Setenv("FRONTEND_URL", "")
	t.Setenv("PUBLIC_BASE_URL", "")
	output, err := Create_Workflow("relative", "return {ok:true}")
	if err != nil || !strings.Contains(output, "URL: /workflows/") || strings.Contains(output, "localhost") {
		t.Fatalf("%s %v", output, err)
	}
}

func TestWorkflowRequiredArguments(t *testing.T) {
	for _, args := range []map[string]interface{}{nil, {}, {"workflow_id": " "}, {"workflow_id": 123}, {"workflow_id": map[string]string{"id": "abc"}}} {
		output, handled, err := ExecuteWorkflowToolAs(SystemWorkflowActor, "Get_Workflow_Status", args)
		if !handled || output != "" || err == nil || !strings.Contains(err.Error(), "List_Workflows") || !strings.Contains(err.Error(), "does not establish workflow completion") {
			t.Fatalf("args=%v output=%q err=%v", args, output, err)
		}
	}
	if _, _, err := ExecuteWorkflowToolAs(SystemWorkflowActor, "Create_Workflow", map[string]interface{}{"name": 123, "code": "1"}); err == nil {
		t.Fatal("coerced non-string name")
	}
}

func TestWorkflowResultSurvivesStatusPersistence(t *testing.T) {
	useTempWorkflowsDir(t)
	d, err := CreateWorkflow(CreateWorkflowParams{Name: "partial", Code: "1"})
	if err != nil {
		t.Fatal(err)
	}
	dir, _ := existingWorkflowDir(d.ID)
	success := false
	st := &WorkflowStatus{ID: d.ID, Status: "failed", TaskSuccess: &success, Result: []byte(`{"ok":false,"requested":10,"processed":2,"failed":8}`), Error: "discovery failed"}
	if err := writeWorkflowStatus(dir, st); err != nil {
		t.Fatal(err)
	}
	got, err := GetWorkflow(d.ID, false)
	if err != nil || got.TaskSuccess == nil || *got.TaskSuccess || !resultHasFailedCount(got.Result, 8) {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	output, err := Get_Workflow_Status(d.ID)
	if err != nil || !strings.Contains(output, `"failed": 8`) || !strings.Contains(output, "Task success reported by script: false") {
		t.Fatalf("%s %v", output, err)
	}
	st.Status = "completed"
	st.TaskSuccess = nil
	st.Result = nil
	if err := writeWorkflowStatus(dir, st); err != nil {
		t.Fatal(err)
	}
	output, _ = Get_Workflow_Status(d.ID)
	if !strings.Contains(output, "task success is unverified") {
		t.Fatal(output)
	}
}

func resultHasFailedCount(raw json.RawMessage, want int) bool {
	var result struct {
		Failed int `json:"failed"`
	}
	return json.Unmarshal(raw, &result) == nil && result.Failed == want
}
