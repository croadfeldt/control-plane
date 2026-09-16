package controller

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// DesiredInstance represents a CatalogItemInstance parsed from a YAML file.
type DesiredInstance struct {
	Name          string             `yaml:"-"`
	ApiVersion    string             `yaml:"-"`
	DisplayName   string             `yaml:"-"`
	CatalogItemID string             `yaml:"-"`
	UserValues    []DesiredUserValue `yaml:"-"`
	SourceFile    string             `yaml:"-"`
	Labels        map[string]string  `yaml:"-"`
}

// DesiredUserValue represents a user value in a desired instance YAML.
type DesiredUserValue struct {
	Resource string `yaml:"resource" json:"resource"`
	Path     string `yaml:"path" json:"path"`
	Value    any    `yaml:"value" json:"value"`
}

// yamlDocument is the raw YAML structure we parse.
type yamlDocument struct {
	ApiVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name   string            `yaml:"name"`
		Labels map[string]string `yaml:"labels,omitempty"`
	} `yaml:"metadata"`
	Spec struct {
		DisplayName   string             `yaml:"display_name"`
		CatalogItemID string             `yaml:"catalog_item_id"`
		UserValues    []DesiredUserValue `yaml:"user_values"`
	} `yaml:"spec"`
}

// ParseError contains a parse error and the file that caused it.
type ParseError struct {
	File string
	Err  error
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("%s: %s", e.File, e.Err.Error())
}

// ParseResult contains the successfully parsed instances and any errors.
type ParseResult struct {
	Instances []DesiredInstance
	Errors    []ParseError
}

// ParseCatalogItemInstances reads YAML files directly at the given path (non-recursive)
// and parses them as CatalogItemInstance definitions.
// Files are processed in alphabetical order; for duplicate metadata.name,
// the last file alphabetically wins.
func ParseCatalogItemInstances(dir, specPath string) ParseResult {
	result := ParseResult{}

	fullPath := filepath.Join(dir, specPath)

	// Verify the resolved path stays within the repo directory.
	absDir, err := filepath.Abs(dir)
	if err != nil {
		result.Errors = append(result.Errors, ParseError{File: dir, Err: fmt.Errorf("resolve repo dir: %w", err)})
		return result
	}
	absFull, err := filepath.Abs(fullPath)
	if err != nil {
		result.Errors = append(result.Errors, ParseError{File: fullPath, Err: fmt.Errorf("resolve spec path: %w", err)})
		return result
	}
	rel, err := filepath.Rel(absDir, absFull)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		result.Errors = append(result.Errors, ParseError{File: specPath, Err: fmt.Errorf("spec.path escapes repository root")})
		return result
	}

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		result.Errors = append(result.Errors, ParseError{File: fullPath, Err: err})
		return result
	}

	// Collect yaml files and sort alphabetically
	var yamlFiles []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if ext == ".yaml" || ext == ".yml" {
			yamlFiles = append(yamlFiles, name)
		}
	}
	sort.Strings(yamlFiles)

	// Parse files; last-wins for duplicate names
	seen := make(map[string]int) // name -> index in result.Instances
	for _, filename := range yamlFiles {
		filePath := filepath.Join(fullPath, filename)
		data, err := os.ReadFile(filePath)
		if err != nil {
			result.Errors = append(result.Errors, ParseError{File: filename, Err: err})
			continue
		}

		var doc yamlDocument
		if err := yaml.Unmarshal(data, &doc); err != nil {
			result.Errors = append(result.Errors, ParseError{File: filename, Err: err})
			continue
		}

		if doc.Kind != "CatalogItemInstance" {
			result.Errors = append(result.Errors, ParseError{
				File: filename,
				Err:  fmt.Errorf("unexpected kind %q, expected CatalogItemInstance", doc.Kind),
			})
			continue
		}

		if doc.Metadata.Name == "" {
			result.Errors = append(result.Errors, ParseError{
				File: filename,
				Err:  fmt.Errorf("metadata.name is required"),
			})
			continue
		}

		if doc.Spec.CatalogItemID == "" {
			result.Errors = append(result.Errors, ParseError{
				File: filename,
				Err:  fmt.Errorf("spec.catalog_item_id is required"),
			})
			continue
		}

		displayName := doc.Spec.DisplayName
		if displayName == "" {
			displayName = doc.Metadata.Name
		}

		instance := DesiredInstance{
			Name:          doc.Metadata.Name,
			ApiVersion:    doc.ApiVersion,
			DisplayName:   displayName,
			CatalogItemID: doc.Spec.CatalogItemID,
			UserValues:    doc.Spec.UserValues,
			SourceFile:    filepath.Join(specPath, filename),
			Labels:        doc.Metadata.Labels,
		}

		if idx, exists := seen[doc.Metadata.Name]; exists {
			// Last file alphabetically wins
			result.Instances[idx] = instance
		} else {
			seen[doc.Metadata.Name] = len(result.Instances)
			result.Instances = append(result.Instances, instance)
		}
	}

	return result
}
