package mcp

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arietis/multi-agent-coder/internal/agent"
	"github.com/arietis/multi-agent-coder/internal/config"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRunUsesSessionWorkspace(t *testing.T) {
	for _, identity := range []string{"first", "second"} {
		t.Run(identity, func(t *testing.T) {
			root, nested := makeWorkspace(t, identity)
			completer := &recordingCompleter{}
			server, err := newServer(context.Background(), config.Config{}, discardLogger(), completer, nested)
			if err != nil {
				t.Fatal(err)
			}
			session := connectInMemory(t, server.SDKServer())
			health, _ := workspaceCall(t, session, "local_agent_health", nil)
			if health["workspace_status"] != "ready" || health["repository_root"] != root {
				t.Fatalf("health = %#v", health)
			}
			result, _ := workspaceCall(t, session, "local_agent_run", workspaceRunArgs(""))
			if result["status"] != "success" || !strings.Contains(completer.messages[1].Content, "workspace "+identity) {
				t.Fatalf("result = %#v, prompt = %#v", result, completer.messages)
			}
		})
	}
}

func TestRunRequiresWorkspaceSelection(t *testing.T) {
	for _, starter := range []string{"unavailable", "outside-git"} {
		t.Run(starter, func(t *testing.T) {
			root, nested := makeWorkspace(t, "selected")
			_, other := makeWorkspace(t, "other")
			start := ""
			if starter == "outside-git" {
				start = t.TempDir()
			}
			completer := &recordingCompleter{}
			cfg := config.Config{Orchestrator: config.OrchestratorClaudeCode, ClaudeToolErrorFlag: true}
			server, err := newServer(context.Background(), cfg, discardLogger(), completer, start)
			if err != nil {
				t.Fatal(err)
			}
			session := connectInMemory(t, server.SDKServer())
			health, _ := workspaceCall(t, session, "local_agent_health", nil)
			if health["workspace_status"] != "needs_input" || health["repository_root"] != nil {
				t.Fatalf("health = %#v", health)
			}
			for _, supplied := range []string{"", "relative", t.TempDir()} {
				result, isError := workspaceCall(t, session, "local_agent_run", workspaceRunArgs(supplied))
				input, ok := result["workspace_input"].(map[string]any)
				if result["status"] != "needs_input" || isError || !ok || input["question"] == nil || len(completer.messages) != 0 {
					t.Fatalf("result for %q = %#v, isError = %t, calls = %d", supplied, result, isError, len(completer.messages))
				}
				if supplied != "" && (input["directory"] != supplied || input["reason"] == nil) {
					t.Fatalf("invalid workspace input = %#v", input)
				}
				if supplied == "" && start != "" && (input["directory"] != start || input["reason"] == nil) {
					t.Fatalf("failed startup workspace input = %#v", input)
				}
			}
			for _, supplied := range []string{nested, "", root} {
				result, _ := workspaceCall(t, session, "local_agent_run", workspaceRunArgs(supplied))
				if result["status"] != "success" || !strings.Contains(completer.messages[1].Content, "workspace selected") {
					t.Fatalf("result = %#v, prompt = %#v", result, completer.messages)
				}
			}
			health, _ = workspaceCall(t, session, "local_agent_health", nil)
			if health["repository_root"] != root || health["workspace_status"] != "ready" || health["workspace_input"] != nil {
				t.Fatalf("health after binding = %#v", health)
			}
			completer.messages = nil
			result, isError := workspaceCall(t, session, "local_agent_run", workspaceRunArgs(other))
			if result["status"] != "error" || !isError || len(completer.messages) != 0 {
				t.Fatalf("workspace switch = %#v, isError = %t, calls = %d", result, isError, len(completer.messages))
			}
		})
	}
}

func TestConfirmedWorkspaceIsUsedByPi(t *testing.T) {
	root, nested := makeWorkspace(t, "pi")
	runRoutingGit(t, root, "add", ".")
	runRoutingGit(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "initial")
	pi := filepath.Join(t.TempDir(), "fake-pi")
	script := `#!/bin/sh
if [ -n "${GIT_DIR-}" ] || [ -n "${GIT_WORK_TREE-}" ]; then exit 1; fi
IFS= read -r prompt
cat input.txt > result.txt
printf '%s\n' '{"id":"mac-prompt","type":"response","success":true}' '{"type":"agent_end","messages":[{"role":"assistant","content":[{"type":"text","text":"Created result.txt."}]}]}' '{"type":"agent_settled"}'
`
	if err := os.WriteFile(pi, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_DIR", filepath.Join(t.TempDir(), ".git"))
	t.Setenv("GIT_WORK_TREE", t.TempDir())
	cfg := config.Config{CoderBackend: "pi", PiExecutable: pi}
	server, err := NewServer(context.Background(), cfg, discardLogger(), "")
	if err != nil {
		t.Fatal(err)
	}
	session := connectInMemory(t, server.SDKServer())
	result, _ := workspaceCall(t, session, "local_agent_run", workspaceRunArgs(""))
	if result["status"] != "needs_input" {
		t.Fatalf("result without workspace = %#v", result)
	}
	result, _ = workspaceCall(t, session, "local_agent_run", workspaceRunArgs(nested))
	patch, _ := result["patch"].(string)
	if result["status"] != "success" || result["backend"] != "pi" || !strings.Contains(patch, "+workspace pi") {
		t.Fatalf("Pi result = %#v", result)
	}
	if _, err := os.Stat(filepath.Join(root, "result.txt")); !os.IsNotExist(err) {
		t.Fatalf("Pi modified the source working tree: %v", err)
	}
}

func TestConcurrentWorkspaceSelectionsBindOnlyOnce(t *testing.T) {
	_, first := makeWorkspace(t, "first")
	_, second := makeWorkspace(t, "second")
	selection := newWorkspaceSelection(context.Background(), "", func(string) agentRunner {
		return staticRunner{}
	})
	results := make(chan error, 2)
	for _, directory := range []string{first, second} {
		go func() {
			_, _, err := selection.selectWorkspace(context.Background(), directory)
			results <- err
		}()
	}
	firstErr, secondErr := <-results, <-results
	if (firstErr == nil) == (secondErr == nil) {
		t.Fatalf("expected one successful binding, got %v and %v", firstErr, secondErr)
	}
	status, _, _ := selection.health()
	if status != "ready" {
		t.Fatalf("workspace status = %q", status)
	}
}

func makeWorkspace(t *testing.T, identity string) (string, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runRoutingGit(t, root, "init", "-q")
	nested := filepath.Join(root, "subdirectory")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "input.txt"), []byte("workspace "+identity+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root, nested
}

func newServer(ctx context.Context, cfg config.Config, logger *slog.Logger, completer agent.ChatCompleter, directory string) (*Server, error) {
	direct, err := agent.NewDirectBackendForOrchestrator(completer, cfg.Orchestrator)
	if err != nil {
		return nil, err
	}
	selection := newWorkspaceSelection(ctx, directory, func(string) agentRunner {
		return agent.NewRunnerWithRoleBackends(map[agent.Role]agent.Backend{
			agent.RoleCoder: direct, agent.RoleFixer: direct, agent.RoleReviewer: direct, agent.RoleTester: direct,
		})
	})
	return newServerWithWorkspace(cfg, logger, selection), nil
}

func workspaceRunArgs(directory string) map[string]any {
	args := map[string]any{"role": "coder", "task": "task", "files": []string{"input.txt"}}
	if directory != "" {
		args["repository_directory"] = directory
	}
	return args
}

func workspaceCall(t *testing.T, session *sdkmcp.ClientSession, name string, args map[string]any) (map[string]any, bool) {
	t.Helper()
	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return result.StructuredContent.(map[string]any), result.IsError
}
