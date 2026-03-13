package repository

import (
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"testing"

	"github.com/hashicorp/packer-plugin-sdk/acctest"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

//go:embed test-fixtures/template.pkr.hcl
var testDatasourceHCL2Basic string

// Run with: PACKER_ACC=1 go test -count 1 -v ./datasource/repository/data_acc_test.go  -timeout=120m
func TestAccGitRepositoryDatasource(t *testing.T) {
	testCase := &acctest.PluginTestCase{
		Name: "git_repository_basic_test",
		Setup: func() error {
			return nil
		},
		Teardown: func() error {
			return nil
		},
		Template: testDatasourceHCL2Basic,
		Type:     "git-repository",
		Check: func(buildCommand *exec.Cmd, logfile string) error {
			if buildCommand.ProcessState != nil {
				if buildCommand.ProcessState.ExitCode() != 0 {
					return fmt.Errorf("Bad exit code. Logfile: %s", logfile)
				}
			}

			logs, err := os.Open(logfile)
			if err != nil {
				return fmt.Errorf("Unable find %s", logfile)
			}
			defer func(logs *os.File) {
				_ = logs.Close()
			}(logs)

			logsBytes, err := io.ReadAll(logs)
			if err != nil {
				return fmt.Errorf("Unable to read %s", logfile)
			}
			logsString := string(logsBytes)

			headLog := "null.basic-example: head: .*"
			isCleanLog := "null.basic-example: is_clean: [true|false]"
			branchesLog := "null.basic-example: num_branches: [0-9]*"
			tagsLog := "null.basic-example: num_tags: [0-9]*"

			checkMatch(t, logsString, "head", headLog)
			checkMatch(t, logsString, "clean", isCleanLog)
			checkMatch(t, logsString, "branches", branchesLog)
			checkMatch(t, logsString, "tags", tagsLog)

			return nil
		},
	}
	acctest.TestPlugin(t, testCase)
}

func checkMatch(test *testing.T, logs string, checkName string, regex string) {
	if matched, _ := regexp.MatchString(regex, logs); !matched {
		test.Fatalf("logs don't contain expected %s value", checkName)
	}
}

func setupTestRepo(t *testing.T) string {
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}

	w, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}

	repo.Config().


	// Create initial commit
	f, err := os.Create(filepath.Join(dir, "test.txt"))
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	_, err = w.Add("test.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.Commit("initial commit", &git.CommitOptions{})
	if err != nil {
		t.Fatal(err)
	}

	// Create some tags
	repo.CreateTag("v1.0.0", plumbing.NewHash(""), nil)
	repo.CreateTag("v2.0.0", plumbing.NewHash(""), nil)
	repo.CreateTag("not-semver", plumbing.NewHash(""), nil)

	return dir
}

func TestDatasource_Configure(t *testing.T) {
	testCases := []struct {
		name        string
		config      map[string]interface{}
		expectError bool
	}{
		{
			name:        "default configuration",
			config:      map[string]interface{}{},
			expectError: false,
		},
		{
			name: "valid tags filter",
			config: map[string]interface{}{
				"tags_filter": "SemVer",
			},
			expectError: false,
		},
		{
			name: "invalid tags filter",
			config: map[string]interface{}{
				"tags_filter": "Invalid",
			},
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			d := &Datasource{}
			err := d.Configure(tc.config)

			if tc.expectError && err == nil {
				t.Errorf("expected error but got none")
			}
			if !tc.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestDatasource_Execute(t *testing.T) {
	repoPath := setupTestRepo(t)

	testCases := []struct {
		name        string
		config      Config
		expectError bool
		validate    func(t *testing.T, output DatasourceOutput)
	}{
		{
			name: "basic execution",
			config: Config{
				Path: repoPath,
			},
			expectError: false,
			validate: func(t *testing.T, output DatasourceOutput) {
				if output.Head == "" {
					t.Error("expected non-empty Head")
				}
				if !output.IsClean {
					t.Error("expected IsClean to be true")
				}
				if len(output.Tags) != 3 {
					t.Errorf("expected 3 tags, got %d", len(output.Tags))
				}
			},
		},
		{
			name: "semver filter",
			config: Config{
				Path:       repoPath,
				TagsFilter: TagsFilterSemVer,
			},
			expectError: false,
			validate: func(t *testing.T, output DatasourceOutput) {
				if len(output.Tags) != 2 {
					t.Errorf("expected 2 tags, got %d", len(output.Tags))
				}
				expectedTags := []string{"v1.0.0", "v2.0.0"}
				for _, expected := range expectedTags {
					found := false
					for _, tag := range output.Tags {
						if tag == expected {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("expected to find tag %s", expected)
					}
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			d := &Datasource{config: tc.config}
			val, err := d.Execute()

			if tc.expectError && err == nil {
				t.Error("expected error but got none")
				return
			}
			if !tc.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if !tc.expectError {
				var output DatasourceOutput
				outputMap := val.AsValueMap()
				output.Head = outputMap["head"].AsString()
				output.IsClean = outputMap["is_clean"].True()
				output.Tags = make([]string, 0)
				for _, tag := range outputMap["tags"].AsValueSlice() {
					output.Tags = append(output.Tags, tag.AsString())
				}
				tc.validate(t, output)
			}
		})
	}
}

func TestDatasource_filterSemverTags(t *testing.T) {
	testCases := []struct {
		name     string
		tags     []string
		expected []string
	}{
		{
			name:     "valid semver tags",
			tags:     []string{"v1.0.0", "v2.0.0", "v1.1.0"},
			expected: []string{"v1.0.0", "v1.1.0", "v2.0.0"},
		},
		{
			name:     "mixed tags",
			tags:     []string{"v1.0.0", "not-semver", "v2.0.0"},
			expected: []string{"v1.0.0", "v2.0.0"},
		},
		{
			name:     "no valid tags",
			tags:     []string{"tag1", "tag2", "not-semver"},
			expected: []string{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			d := &Datasource{}
			result := d.filterSemverTags(tc.tags)
			if !reflect.DeepEqual(tc.expected, result) {
				t.Errorf("expected %v, got %v", tc.expected, result)
			}
		})
	}
}
