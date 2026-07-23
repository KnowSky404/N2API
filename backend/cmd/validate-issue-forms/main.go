// Command validate-issue-forms checks N2API issue templates against GitHub's documented form schema.
package main

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

var issueFormIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func main() {
	directory := ".github/ISSUE_TEMPLATE"
	if len(os.Args) > 2 {
		fmt.Fprintln(os.Stderr, "usage: validate-issue-forms [issue-template-directory]")
		os.Exit(2)
	}
	if len(os.Args) == 2 {
		directory = os.Args[1]
	}
	if err := validateIssueTemplateDirectory(directory); err != nil {
		fmt.Fprintf(os.Stderr, "issue form validation failed: %v\n", err)
		os.Exit(1)
	}
}

func validateIssueTemplateDirectory(directory string) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("read template directory: %w", err)
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		extension := strings.ToLower(filepath.Ext(entry.Name()))
		if extension == ".yml" || extension == ".yaml" {
			paths = append(paths, filepath.Join(directory, entry.Name()))
		}
	}
	sort.Strings(paths)

	formCount := 0
	configCount := 0
	formNames := make(map[string]string)
	for _, path := range paths {
		root, err := loadSingleYAMLDocument(path)
		if err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(path), err)
		}
		base := strings.ToLower(filepath.Base(path))
		if base == "config.yml" || base == "config.yaml" {
			configCount++
			err = validateIssueTemplateConfig(root)
		} else {
			formCount++
			var formName string
			formName, err = validateIssueForm(root)
			if err == nil {
				if previous, exists := formNames[formName]; exists {
					return fmt.Errorf("%s: form.name %q duplicates %s", filepath.Base(path), formName, previous)
				}
				formNames[formName] = filepath.Base(path)
			}
		}
		if err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(path), err)
		}
	}
	if configCount != 1 {
		return fmt.Errorf("expected exactly one config.yml or config.yaml, found %d", configCount)
	}
	if formCount == 0 {
		return errors.New("expected at least one issue form")
	}
	return nil
}

func loadSingleYAMLDocument(path string) (*yaml.Node, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	decoder := yaml.NewDecoder(file)
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	var trailing yaml.Node
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, errors.New("multiple YAML documents are not allowed")
		}
		return nil, err
	}
	if document.Kind != yaml.DocumentNode || len(document.Content) != 1 {
		return nil, errors.New("expected one YAML document")
	}
	return document.Content[0], nil
}

func validateIssueForm(root *yaml.Node) (string, error) {
	fields, err := mappingFields(root, "form", "name", "description", "title", "labels", "assignees", "projects", "type", "body")
	if err != nil {
		return "", err
	}
	name, err := requiredNonEmptyString(fields, "name", "form")
	if err != nil {
		return "", err
	}
	for _, field := range []string{"name", "description"} {
		if _, err := requiredNonEmptyString(fields, field, "form"); err != nil {
			return "", err
		}
	}
	for _, field := range []string{"title", "type"} {
		if node := fields[field]; node != nil {
			if _, err := scalarString(node, "form."+field, true); err != nil {
				return "", err
			}
		}
	}
	for _, field := range []string{"labels", "assignees", "projects"} {
		if node := fields[field]; node != nil {
			if err := uniqueStringSequence(node, "form."+field, true); err != nil {
				return "", err
			}
		}
	}
	body := fields["body"]
	if body == nil || body.Kind != yaml.SequenceNode || len(body.Content) == 0 {
		return "", errors.New("form.body must be a non-empty sequence")
	}
	seenIDs := make(map[string]struct{})
	hasInput := false
	for index, element := range body.Content {
		elementType, err := validateIssueFormElement(element, index, seenIDs)
		if err != nil {
			return "", err
		}
		if elementType != "markdown" {
			hasInput = true
		}
	}
	if !hasInput {
		return "", errors.New("form.body must contain at least one input element")
	}
	return name, nil
}

func validateIssueFormElement(node *yaml.Node, index int, seenIDs map[string]struct{}) (string, error) {
	context := fmt.Sprintf("form.body[%d]", index)
	fields, err := mappingFields(node, context, "type", "id", "attributes", "validations")
	if err != nil {
		return "", err
	}
	elementType, err := requiredNonEmptyString(fields, "type", context)
	if err != nil {
		return "", err
	}
	switch elementType {
	case "markdown":
		if fields["id"] != nil || fields["validations"] != nil {
			return "", fmt.Errorf("%s markdown elements must not define id or validations", context)
		}
		return elementType, validateMarkdownAttributes(fields["attributes"], context+".attributes")
	case "input", "textarea", "dropdown", "checkboxes", "upload":
	default:
		return "", fmt.Errorf("%s.type has unsupported value %q", context, elementType)
	}

	id, err := requiredNonEmptyString(fields, "id", context)
	if err != nil {
		return "", err
	}
	if !issueFormIDPattern.MatchString(id) {
		return "", fmt.Errorf("%s.id must contain only letters, numbers, hyphens, or underscores", context)
	}
	if _, exists := seenIDs[id]; exists {
		return "", fmt.Errorf("%s.id %q is duplicated", context, id)
	}
	seenIDs[id] = struct{}{}
	if fields["validations"] != nil {
		allowed := []string{"required"}
		if elementType == "upload" {
			allowed = append(allowed, "accept")
		}
		validationFields, err := mappingFields(fields["validations"], context+".validations", allowed...)
		if err != nil {
			return "", err
		}
		if required := validationFields["required"]; required != nil {
			if _, err := scalarBool(required, context+".validations.required"); err != nil {
				return "", err
			}
		}
		if accept := validationFields["accept"]; accept != nil {
			if _, err := scalarString(accept, context+".validations.accept", false); err != nil {
				return "", err
			}
		}
	}

	switch elementType {
	case "input":
		return elementType, validateTextAttributes(fields["attributes"], context+".attributes", false)
	case "textarea":
		return elementType, validateTextAttributes(fields["attributes"], context+".attributes", true)
	case "dropdown":
		return elementType, validateDropdownAttributes(fields["attributes"], context+".attributes")
	case "checkboxes":
		return elementType, validateCheckboxAttributes(fields["attributes"], context+".attributes")
	default:
		return elementType, validateUploadAttributes(fields["attributes"], context+".attributes")
	}
}

func validateMarkdownAttributes(node *yaml.Node, context string) error {
	fields, err := mappingFields(node, context, "value")
	if err != nil {
		return err
	}
	_, err = requiredNonEmptyString(fields, "value", context)
	return err
}

func validateTextAttributes(node *yaml.Node, context string, textarea bool) error {
	allowed := []string{"label", "description", "placeholder", "value"}
	if textarea {
		allowed = append(allowed, "render")
	}
	fields, err := mappingFields(node, context, allowed...)
	if err != nil {
		return err
	}
	if _, err := requiredNonEmptyString(fields, "label", context); err != nil {
		return err
	}
	for _, field := range allowed[1:] {
		if value := fields[field]; value != nil {
			if _, err := scalarString(value, context+"."+field, true); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateDropdownAttributes(node *yaml.Node, context string) error {
	fields, err := mappingFields(node, context, "label", "description", "options", "multiple", "default")
	if err != nil {
		return err
	}
	if _, err := requiredNonEmptyString(fields, "label", context); err != nil {
		return err
	}
	if description := fields["description"]; description != nil {
		if _, err := scalarString(description, context+".description", true); err != nil {
			return err
		}
	}
	options := fields["options"]
	if options == nil || options.Kind != yaml.SequenceNode || len(options.Content) == 0 {
		return fmt.Errorf("%s.options must be a non-empty sequence", context)
	}
	if err := uniqueStringSequence(options, context+".options", false); err != nil {
		return err
	}
	if multiple := fields["multiple"]; multiple != nil {
		if _, err := scalarBool(multiple, context+".multiple"); err != nil {
			return err
		}
	}
	if defaultValue := fields["default"]; defaultValue != nil {
		defaultIndex, err := scalarInt(defaultValue, context+".default")
		if err != nil {
			return err
		}
		if defaultIndex < 0 || defaultIndex >= len(options.Content) {
			return fmt.Errorf("%s.default must index an option", context)
		}
	}
	return nil
}

func validateCheckboxAttributes(node *yaml.Node, context string) error {
	fields, err := mappingFields(node, context, "label", "description", "options")
	if err != nil {
		return err
	}
	if _, err := requiredNonEmptyString(fields, "label", context); err != nil {
		return err
	}
	if description := fields["description"]; description != nil {
		if _, err := scalarString(description, context+".description", true); err != nil {
			return err
		}
	}
	options := fields["options"]
	if options == nil || options.Kind != yaml.SequenceNode || len(options.Content) == 0 {
		return fmt.Errorf("%s.options must be a non-empty sequence", context)
	}
	seenLabels := make(map[string]struct{}, len(options.Content))
	for index, option := range options.Content {
		optionContext := fmt.Sprintf("%s.options[%d]", context, index)
		optionFields, err := mappingFields(option, optionContext, "label", "required")
		if err != nil {
			return err
		}
		label, err := requiredNonEmptyString(optionFields, "label", optionContext)
		if err != nil {
			return err
		}
		if _, exists := seenLabels[label]; exists {
			return fmt.Errorf("%s.label %q is duplicated", optionContext, label)
		}
		seenLabels[label] = struct{}{}
		if required := optionFields["required"]; required != nil {
			if _, err := scalarBool(required, optionContext+".required"); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateUploadAttributes(node *yaml.Node, context string) error {
	fields, err := mappingFields(node, context, "label", "description")
	if err != nil {
		return err
	}
	if _, err := requiredNonEmptyString(fields, "label", context); err != nil {
		return err
	}
	if description := fields["description"]; description != nil {
		if _, err := scalarString(description, context+".description", true); err != nil {
			return err
		}
	}
	return nil
}

func validateIssueTemplateConfig(root *yaml.Node) error {
	fields, err := mappingFields(root, "config", "blank_issues_enabled", "contact_links")
	if err != nil {
		return err
	}
	if blank := fields["blank_issues_enabled"]; blank != nil {
		if _, err := scalarBool(blank, "config.blank_issues_enabled"); err != nil {
			return err
		}
	}
	links := fields["contact_links"]
	if links == nil {
		return nil
	}
	if links.Kind != yaml.SequenceNode {
		return errors.New("config.contact_links must be a sequence")
	}
	for index, link := range links.Content {
		context := fmt.Sprintf("config.contact_links[%d]", index)
		linkFields, err := mappingFields(link, context, "name", "url", "about")
		if err != nil {
			return err
		}
		for _, field := range []string{"name", "url", "about"} {
			if _, err := requiredNonEmptyString(linkFields, field, context); err != nil {
				return err
			}
		}
		linkURL, _ := scalarString(linkFields["url"], context+".url", false)
		parsed, err := url.ParseRequestURI(linkURL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return fmt.Errorf("%s.url must be an absolute HTTPS URL", context)
		}
	}
	return nil
}

func mappingFields(node *yaml.Node, context string, allowed ...string) (map[string]*yaml.Node, error) {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s must be a mapping", context)
	}
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, field := range allowed {
		allowedSet[field] = struct{}{}
	}
	fields := make(map[string]*yaml.Node, len(node.Content)/2)
	for index := 0; index < len(node.Content); index += 2 {
		keyNode := node.Content[index]
		valueNode := node.Content[index+1]
		if keyNode.Kind != yaml.ScalarNode || keyNode.Tag != "!!str" {
			return nil, fmt.Errorf("%s contains a non-string key", context)
		}
		key := keyNode.Value
		if _, exists := fields[key]; exists {
			return nil, fmt.Errorf("%s contains duplicate key %q", context, key)
		}
		if _, ok := allowedSet[key]; !ok {
			return nil, fmt.Errorf("%s contains unknown field %q", context, key)
		}
		fields[key] = valueNode
	}
	return fields, nil
}

func requiredNonEmptyString(fields map[string]*yaml.Node, field, context string) (string, error) {
	node := fields[field]
	if node == nil {
		return "", fmt.Errorf("%s.%s is required", context, field)
	}
	return scalarString(node, context+"."+field, false)
}

func scalarString(node *yaml.Node, context string, allowEmpty bool) (string, error) {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!str" {
		return "", fmt.Errorf("%s must be a string", context)
	}
	if !allowEmpty && strings.TrimSpace(node.Value) == "" {
		return "", fmt.Errorf("%s must not be empty", context)
	}
	return node.Value, nil
}

func scalarBool(node *yaml.Node, context string) (bool, error) {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!bool" {
		return false, fmt.Errorf("%s must be a boolean", context)
	}
	return node.Value == "true", nil
}

func scalarInt(node *yaml.Node, context string) (int, error) {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!int" {
		return 0, fmt.Errorf("%s must be an integer", context)
	}
	var value int
	if err := node.Decode(&value); err != nil {
		return 0, fmt.Errorf("%s must be an integer", context)
	}
	return value, nil
}

func uniqueStringSequence(node *yaml.Node, context string, allowEmpty bool) error {
	if node.Kind != yaml.SequenceNode {
		return fmt.Errorf("%s must be a sequence", context)
	}
	if !allowEmpty && len(node.Content) == 0 {
		return fmt.Errorf("%s must be a non-empty sequence", context)
	}
	seen := make(map[string]struct{}, len(node.Content))
	for index, item := range node.Content {
		value, err := scalarString(item, fmt.Sprintf("%s[%d]", context, index), false)
		if err != nil {
			return err
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("%s[%d] value %q is duplicated", context, index, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}
