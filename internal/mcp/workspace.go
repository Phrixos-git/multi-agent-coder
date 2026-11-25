package mcp

import (
	"context"
	"errors"
	"sync"

	"github.com/arietis/multi-agent-coder/internal/workspace"
)

type WorkspaceInput struct {
	Question  string `json:"question"`
	Directory string `json:"directory,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

type boundWorkspace struct {
	root   string
	reader *workspace.Reader
	runner agentRunner
}

type workspaceSelection struct {
	mu            sync.Mutex
	bound         *boundWorkspace
	startupInput  *WorkspaceInput
	runnerForRoot func(string) agentRunner
}

func newWorkspaceSelection(ctx context.Context, directory string, runnerForRoot func(string) agentRunner) *workspaceSelection {
	selection := &workspaceSelection{runnerForRoot: runnerForRoot}
	if directory != "" {
		_, selection.startupInput, _ = selection.selectWorkspace(ctx, directory)
	}
	if selection.bound == nil && selection.startupInput == nil {
		selection.startupInput = workspaceQuestion("", nil)
	}
	return selection
}

func (s *workspaceSelection) selectWorkspace(ctx context.Context, directory string) (*boundWorkspace, *WorkspaceInput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if directory == "" {
		if s.bound != nil {
			return s.bound, nil, nil
		}
		return nil, s.startupInput, nil
	}

	root, err := workspace.ResolveRepositoryRoot(ctx, directory)
	if s.bound != nil {
		if err != nil || root != s.bound.root {
			return nil, nil, &workspace.WorkspaceError{
				Operation: "select repository", Path: directory,
				Err: errors.New("this MCP session is already bound to " + s.bound.root + "; open a new session to change repositories"),
			}
		}
		return s.bound, nil, nil
	}
	if err != nil {
		return nil, workspaceQuestion(directory, err), nil
	}
	reader, err := workspace.NewReader(root)
	if err != nil {
		return nil, workspaceQuestion(directory, err), nil
	}
	s.bound = &boundWorkspace{root: root, reader: reader, runner: s.runnerForRoot(root)}
	return s.bound, nil, nil
}

func (s *workspaceSelection) health() (status, root string, input *WorkspaceInput) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bound != nil {
		return "ready", s.bound.root, nil
	}
	return "needs_input", "", s.startupInput
}

func (s *workspaceSelection) run(ctx context.Context, input RunInput) RunOutput {
	bound, question, err := s.selectWorkspace(ctx, input.RepositoryDirectory)
	if err != nil {
		return failedOutput(input.Role, err)
	}
	if question != nil {
		return RunOutput{
			Status: "needs_input", Role: input.Role,
			Summary:   "Ask the user which Git repository to use. Retry the same task with the directory they choose in repository_directory.",
			Artifacts: []Artifact{}, Findings: []Finding{}, Risks: []string{}, Notes: []string{},
			WorkspaceInput: question,
		}
	}
	return executeRun(ctx, bound.runner, bound.reader, input)
}

func workspaceQuestion(directory string, err error) *WorkspaceInput {
	input := &WorkspaceInput{Question: "Which Git repository should MAC use? Specify an absolute directory path.", Directory: directory}
	if err != nil {
		input.Reason = err.Error()
	}
	return input
}
