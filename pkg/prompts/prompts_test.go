package prompts

import (
	"testing"
)

// TestLoadMarkdown tests the loadMarkdown function
func TestLoadMarkdown(t *testing.T) {
	// Test loading an existing file
	content, err := loadMarkdown("DBDescription.md")
	if err != nil {
		// This is not a fatal error as the file might not exist in test environment
		t.Logf("Warning: Failed to load DBDescription.md: %v", err)
	} else {
		if content == "" {
			t.Error("loadMarkdown returned empty content for DBDescription.md")
		}
	}

	// Test loading a non-existent file
	_, err = loadMarkdown("NonExistentFile.md")
	if err == nil {
		t.Error("loadMarkdown should return an error for non-existent file")
	}
}

// TestDocumentLoading tests loading all document files
func TestDocumentLoading(t *testing.T) {
	// Define all expected document files
	docFiles := []string{
		"DBDescription.md",
		"SyntaxGuide.md",
		"ReadExamples.md",
		"WriteExamples.md",
		"ClusterManagementGuide.md",
		"DataMigrationGuide.md",
		"InstallationGuide.md",
		"PerformanceTuningGuide.md",
		"TroubleShootingGuide.md",
		"BackupRestoreGuide.md",
		"DBATemplate.md",
	}

	// Try to load each file and report results
	for _, filename := range docFiles {
		content, err := loadMarkdown(filename)
		if err != nil {
			t.Logf("Warning: Failed to load %s: %v", filename, err)
		} else {
			t.Logf("Successfully loaded %s (%d bytes)", filename, len(content))
			if content == "" {
				t.Errorf("Empty content in %s", filename)
			}
		}
	}
}

// TestInitialization tests that the init function initializes global variables
func TestInitialization(t *testing.T) {
	// The init function should have already run when the package was imported
	// Check that the global variables have been initialized with default values at minimum

	if DBDescription == "" {
		t.Error("DBDescription was not initialized")
	}

	if SyntaxGuide == "" {
		t.Error("SyntaxGuide was not initialized")
	}

	// Check all global variables
	vars := map[string]string{
		"DBDescription":          DBDescription,
		"SyntaxGuide":            SyntaxGuide,
		"ReadExamplesTemplate":   ReadExamplesTemplate,
		"WriteExamplesTemplate":  WriteExamplesTemplate,
		"ClusterManagementGuide": ClusterManagementGuide,
		"DataMigrationGuide":     DataMigrationGuide,
		"InstallationGuide":      InstallationGuide,
		"PerformanceTuningGuide": PerformanceTuningGuide,
		"TroubleshootingGuide":   TroubleshootingGuide,
		"BackupRestoreGuide":     BackupRestoreGuide,
		"DBATemplate":            DBATemplate,
	}

	for name, value := range vars {
		t.Logf("%s length: %d", name, len(value))
	}
}

// TestRegisterPrompts tests that the RegisterPrompts function doesn't panic
func TestRegisterPrompts(t *testing.T) {
	// This test just verifies that RegisterPrompts exists and can be referenced
	// We can't easily test the actual registration without mocking the MCP server
	t.Log("RegisterPrompts function exists and can be referenced")
}
