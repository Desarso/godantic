package common_tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestWorkflowE2ERealExecutor runs the real pnpm/tsx workflow executor.
// Opt-in: WORKFLOW_E2E=1 and WORKFLOW_E2E_ROOT=<dir containing helpers/typescript_runtime
// with node_modules installed>.
func TestWorkflowE2ERealExecutor(t *testing.T) {
	root := os.Getenv("WORKFLOW_E2E_ROOT")
	if os.Getenv("WORKFLOW_E2E") != "1" || root == "" {
		t.Skip("set WORKFLOW_E2E=1 and WORKFLOW_E2E_ROOT to run")
	}
	wd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	useTempWorkflowsDir(t)
	t.Setenv("TS_RUNTIME_TOOLS", "web,math")
	t.Setenv("DATABASE_URL", "postgres://must-not-leak")

	logsOf := func(id string) string { l, _ := ReadWorkflowLogs(id, 0); return l }
	statusOf := func(id string) *WorkflowDetails { d, _ := GetWorkflow(id, false); return d }

	// 1. Successful run with helpers, process shadowed, env not leaked.
	ok, err := CreateWorkflow(CreateWorkflowParams{Name: "ok", Code: `
const x: number = math.sqrt(16);
console.log("sqrt", x);
console.log("process is", typeof process, "require is", typeof require);
console.log("globalThis.process is", typeof globalThis.process);
return { done: true };
`})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RunWorkflow(ok.ID, "manual"); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, 60*time.Second, func() bool { return !IsWorkflowRunning(ok.ID) }) {
		t.Fatal("did not finish")
	}
	d := statusOf(ok.ID)
	logs := logsOf(ok.ID)
	t.Logf("status=%s err=%q\n%s", d.Status, d.Error, logs)
	if d.Status != "completed" || !strings.Contains(logs, "sqrt 4") || !strings.Contains(logs, "process is undefined require is undefined") || !strings.Contains(logs, "globalThis.process is undefined") {
		t.Fatalf("unexpected result")
	}

	// 2. Blocklist.
	bad, _ := CreateWorkflow(CreateWorkflowParams{Name: "bad", Code: `const fs = require("fs");`})
	RunWorkflow(bad.ID, "manual")
	waitFor(t, 60*time.Second, func() bool { return !IsWorkflowRunning(bad.ID) })
	if d := statusOf(bad.ID); d.Status != "failed" || !strings.Contains(d.Error, "forbidden") {
		t.Fatalf("blocklist: %+v", d)
	}

	// 3. Long-running workflow is stopped (whole group).
	long, _ := CreateWorkflow(CreateWorkflowParams{Name: "long", Code: `
console.log("sleeping");
await new Promise(r => setTimeout(r, 60000));
`})
	pid, err := RunWorkflow(long.ID, "manual")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, 30*time.Second, func() bool { return strings.Contains(logsOf(long.ID), "sleeping") })
	if err := StopWorkflow(long.ID); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, 3*time.Second, func() bool { return !workflowProcessAlive(-pid) }) {
		t.Fatal("process group survived stop")
	}
	if d := statusOf(long.ID); d.Status != "failed" || d.Error != "Stopped by user" {
		t.Fatalf("stop: %+v", d)
	}
	stderr, _ := os.ReadFile(filepath.Join(workflowsDir, ok.ID, "stderr.txt"))
	if strings.Contains(string(stderr), "must-not-leak") {
		t.Fatal("env leaked")
	}
}

func TestWorkflowE2EOutcomesAndDiagnostics(t *testing.T) {
	root := os.Getenv("WORKFLOW_E2E_ROOT")
	if os.Getenv("WORKFLOW_E2E") != "1" || root == "" {
		t.Skip("set WORKFLOW_E2E=1 and WORKFLOW_E2E_ROOT to run")
	}
	wd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	useTempWorkflowsDir(t)
	t.Setenv("TS_RUNTIME_TOOLS", "web,math")
	for _, tc := range []struct {
		name, code, status, diagnostic string
		success                        *bool
	}{
		{"failure", `return {ok:false, error:"SharePoint discovery failed", requested:5, processed:0, failed:5};`, "failed", "SharePoint discovery failed", boolPtr(false)},
		{"partial", `return {ok:true, complete:false, processed:2, failed:3};`, "failed", "incomplete work", boolPtr(false)},
		{"unknown", `return {ok:true, unknown:1};`, "failed", "incomplete work", boolPtr(false)},
		{"cyclic", `const result: any = {ok:true}; result.self = result; return result;`, "failed", "JSON-serializable", boolPtr(false)},
		{"throw", `throw new Error("Graph 503");`, "failed", "Graph 503", boolPtr(false)},
		{"unverified", `console.log("as const remains literal");`, "completed", "", nil},
		{"typed", "interface Result {ok:boolean; complete:boolean; verified:number}\nconst result: Result = {ok:true, complete:true, verified:math.sqrt(4)};\nreturn result;", "completed", "", boolPtr(true)},
		{"parse", "console.log('must not run');\nconst broken = ;", "failed", "line 2, column 16", boolPtr(false)},
		{"eof", "console.log('must not run');\nif (true) {", "failed", "line 2, column 12", boolPtr(false)},
		{"policy", "console.log('must not run');\nprocess.exit(0);", "failed", "line 2, column 1", boolPtr(false)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := CreateWorkflow(CreateWorkflowParams{Name: tc.name, Code: tc.code})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := RunWorkflow(d.ID, "manual"); err != nil {
				t.Fatal(err)
			}
			if !waitFor(t, 30*time.Second, func() bool { return !IsWorkflowRunning(d.ID) }) {
				_ = StopWorkflow(d.ID)
				t.Fatal("did not finish")
			}
			got, err := GetWorkflow(d.ID, false)
			if err != nil || got.Status != tc.status || (tc.diagnostic != "" && !strings.Contains(strings.ToLower(got.Error), strings.ToLower(tc.diagnostic))) {
				t.Fatalf("got=%+v err=%v", got, err)
			}
			if (got.TaskSuccess == nil) != (tc.success == nil) || (tc.success != nil && *got.TaskSuccess != *tc.success) {
				t.Fatalf("task success = %v", got.TaskSuccess)
			}
			logs, _ := ReadWorkflowLogs(d.ID, 0)
			if strings.Contains(logs, "must not run") && strings.Contains(logs, "] must not run") {
				t.Fatal("invalid code executed before compilation")
			}
			if tc.name == "failure" && (!resultHasFailedCount(got.Result, 5) || got.ExitCode == nil || *got.ExitCode == 0) {
				t.Fatalf("failure result lost: %+v", got)
			}
			if tc.name == "parse" && (!strings.Contains(got.Error, "const broken = ;") || !strings.Contains(got.Error, d.ID)) {
				t.Fatal(got.Error)
			}
			if tc.name == "unverified" && !strings.Contains(logs, "as const remains literal") {
				t.Fatal("compiler corrupted string literal")
			}
		})
	}
}

func boolPtr(value bool) *bool { return &value }
