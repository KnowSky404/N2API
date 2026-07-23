package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validIssueForm = `name: Bug report
description: Report a defect
title: "[Bug]: "
labels:
  - bug
type: Bug
body:
  - type: markdown
    attributes:
      value: Read this first.
  - type: input
    id: version
    attributes:
      label: Version
    validations:
      required: true
  - type: dropdown
    id: deployment
    attributes:
      label: Deployment
      options:
        - Compose
        - Source
      default: 0
  - type: checkboxes
    id: checks
    attributes:
      label: Checks
      options:
        - label: I removed secrets.
          required: true
  - type: upload
    id: evidence
    attributes:
      label: Sanitized evidence
    validations:
      required: false
      accept: ".png,.txt"
`

const validIssueConfig = `blank_issues_enabled: false
contact_links:
  - name: Private report
    url: https://example.test/security
    about: Report vulnerabilities privately.
`

func TestValidateRepositoryIssueForms(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateIssueTemplateDirectory(filepath.Join(repoRoot, ".github", "ISSUE_TEMPLATE")); err != nil {
		t.Fatalf("repository issue forms are invalid: %v", err)
	}
}

func TestValidateIssueTemplateDirectory(t *testing.T) {
	tests := []struct {
		name        string
		form        string
		config      string
		wantErrPart string
	}{
		{name: "valid", form: validIssueForm, config: validIssueConfig},
		{name: "missing name", form: strings.Replace(validIssueForm, "name: Bug report\n", "", 1), config: validIssueConfig, wantErrPart: "form.name is required"},
		{name: "unknown field", form: strings.Replace(validIssueForm, "description: Report a defect\n", "description: Report a defect\nunknown: true\n", 1), config: validIssueConfig, wantErrPart: "unknown field"},
		{name: "duplicate mapping key", form: strings.Replace(validIssueForm, "description: Report a defect\n", "description: Report a defect\ndescription: Duplicate\n", 1), config: validIssueConfig, wantErrPart: "duplicate key"},
		{name: "duplicate id", form: strings.Replace(validIssueForm, "id: deployment", "id: version", 1), config: validIssueConfig, wantErrPart: "is duplicated"},
		{name: "duplicate dropdown option", form: strings.Replace(validIssueForm, "        - Source", "        - Compose", 1), config: validIssueConfig, wantErrPart: "is duplicated"},
		{name: "invalid required type", form: strings.Replace(validIssueForm, "required: true", "required: yes", 1), config: validIssueConfig, wantErrPart: "must be a boolean"},
		{name: "multiple documents", form: validIssueForm + "---\nname: Extra\n", config: validIssueConfig, wantErrPart: "multiple YAML documents"},
		{name: "insecure contact URL", form: validIssueForm, config: strings.Replace(validIssueConfig, "https://", "http://", 1), wantErrPart: "absolute HTTPS URL"},
		{name: "unknown config field", form: validIssueForm, config: validIssueConfig + "unexpected: true\n", wantErrPart: "unknown field"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			writeTestFile(t, filepath.Join(directory, "bug.yml"), test.form)
			writeTestFile(t, filepath.Join(directory, "config.yml"), test.config)
			err := validateIssueTemplateDirectory(directory)
			if test.wantErrPart == "" {
				if err != nil {
					t.Fatalf("validation failed: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErrPart) {
				t.Fatalf("validation error = %v, want substring %q", err, test.wantErrPart)
			}
		})
	}
}

func TestValidateIssueTemplateDirectoryRequiresConfigAndForm(t *testing.T) {
	directory := t.TempDir()
	writeTestFile(t, filepath.Join(directory, "bug.yml"), validIssueForm)
	if err := validateIssueTemplateDirectory(directory); err == nil || !strings.Contains(err.Error(), "exactly one config") {
		t.Fatalf("missing config error = %v", err)
	}

	directory = t.TempDir()
	writeTestFile(t, filepath.Join(directory, "config.yml"), validIssueConfig)
	if err := validateIssueTemplateDirectory(directory); err == nil || !strings.Contains(err.Error(), "at least one issue form") {
		t.Fatalf("missing form error = %v", err)
	}
}

func TestValidateIssueTemplateDirectoryRejectsDuplicateFormNames(t *testing.T) {
	directory := t.TempDir()
	writeTestFile(t, filepath.Join(directory, "bug.yml"), validIssueForm)
	writeTestFile(t, filepath.Join(directory, "feature.yml"), validIssueForm)
	writeTestFile(t, filepath.Join(directory, "config.yml"), validIssueConfig)
	if err := validateIssueTemplateDirectory(directory); err == nil || !strings.Contains(err.Error(), "duplicates") {
		t.Fatalf("duplicate form name error = %v", err)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
