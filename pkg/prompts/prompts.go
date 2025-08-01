package prompts

import (
	"context"
	"embed"
	"fmt"
	"strings"

	"gitee.com/kwdb/kwdb-mcp-server/pkg/db"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

//go:embed docs/*.md
var docsFS embed.FS

// loadMarkdown loads a Markdown file from the embedded file system
func loadMarkdown(filename string) (string, error) {
	content, err := docsFS.ReadFile(fmt.Sprintf("docs/%s", filename))
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %v", filename, err)
	}
	return string(content), nil
}

// DBDescription is the database description provided to the LLM
var DBDescription string

// SyntaxGuide is the KWDB syntax guide
var SyntaxGuide string

// ReadExamplesTemplate is the template for read query examples
var ReadExamplesTemplate string

// WriteExamplesTemplate is the template for write query examples
var WriteExamplesTemplate string

// Use case guide content
var ClusterManagementGuide string
var DataMigrationGuide string
var InstallationGuide string
var PerformanceTuningGuide string
var TroubleshootingGuide string

// Additional guides
var BackupRestoreGuide string
var DBATemplate string

// init function loads all Markdown files when the package is imported
func init() {
	var err error

	// Load database description
	DBDescription, err = loadMarkdown("DBDescription.md")
	if err != nil {
		// Provide default content if file loading fails
		DBDescription = "# KWDB Database\n\nKWDB is a distributed SQL database compatible with PostgreSQL and CockroachDB."
		fmt.Printf("Warning: Failed to load database description file: %v\n", err)
	}

	// Load syntax guide
	SyntaxGuide, err = loadMarkdown("SyntaxGuide.md")
	if err != nil {
		// Provide default content if file loading fails
		SyntaxGuide = "# KWDB SQL Syntax Guide\n\nKWDB supports standard SQL syntax compatible with PostgreSQL and CockroachDB."
		fmt.Printf("Warning: Failed to load syntax guide file: %v\n", err)
	}

	// Load read query examples template
	ReadExamplesTemplate, err = loadMarkdown("ReadExamples.md")
	if err != nil {
		// Provide default content if file loading fails
		ReadExamplesTemplate = ""
		fmt.Printf("Warning: Failed to load read examples file: %v\n", err)
	}

	// Load write query examples template
	WriteExamplesTemplate, err = loadMarkdown("WriteExamples.md")
	if err != nil {
		// Provide default content if file loading fails
		WriteExamplesTemplate = ""
		fmt.Printf("Warning: Failed to load write examples file: %v\n", err)
	}

	// Load use case guides
	// Note: These files serve as templates for generating prompts
	// When implementing auto-generation logic, refer to these templates
	// and follow the same structure for new prompt templates

	// Cluster management guide
	ClusterManagementGuide, err = loadMarkdown("ClusterManagementGuide.md")
	if err != nil {
		// Provide default content if file loading fails
		ClusterManagementGuide = ""
		fmt.Printf("Warning: Failed to load cluster management guide file: %v\n", err)
	}

	// Data migration guide
	DataMigrationGuide, err = loadMarkdown("DataMigrationGuide.md")
	if err != nil {
		// Provide default content if file loading fails
		DataMigrationGuide = ""
		fmt.Printf("Warning: Failed to load data migration guide file: %v\n", err)
	}

	// Installation guide
	InstallationGuide, err = loadMarkdown("InstallationGuide.md")
	if err != nil {
		// Provide default content if file loading fails
		InstallationGuide = ""
		fmt.Printf("Warning: Failed to load installation guide file: %v\n", err)
	}

	// Performance tuning guide
	PerformanceTuningGuide, err = loadMarkdown("PerformanceTuningGuide.md")
	if err != nil {
		// Provide default content if file loading fails
		PerformanceTuningGuide = ""
		fmt.Printf("Warning: Failed to load performance tuning guide file: %v\n", err)
	}

	// Troubleshooting guide
	TroubleshootingGuide, err = loadMarkdown("TroubleShootingGuide.md")
	if err != nil {
		// Provide default content if file loading fails
		TroubleshootingGuide = ""
		fmt.Printf("Warning: Failed to load troubleshooting guide file: %v\n", err)
	}

	// Backup and restore guide
	BackupRestoreGuide, err = loadMarkdown("BackupRestoreGuide.md")
	if err != nil {
		// Provide default content if file loading fails
		BackupRestoreGuide = ""
		fmt.Printf("Warning: Failed to load backup and restore guide file: %v\n", err)
	}

	// DBA template
	DBATemplate, err = loadMarkdown("DBATemplate.md")
	if err != nil {
		// Provide default content if file loading fails
		DBATemplate = ""
		fmt.Printf("Warning: Failed to load DBA template file: %v\n", err)
	}
}

// GetReadExampleQueries returns example read queries for a table
// This function demonstrates how to use the ReadExamplesTemplate
// to generate table-specific queries by replacing placeholders
func GetReadExampleQueries(tableName string) []string {
	// Replace {table} placeholder with actual table name
	content := strings.ReplaceAll(ReadExamplesTemplate, "{table}", tableName)

	// Split content by lines and remove "- " prefix
	lines := strings.Split(content, "\n")
	var queries []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = strings.TrimPrefix(line, "- ")
		queries = append(queries, line)
	}

	return queries
}

// GetWriteExampleQueries returns example write queries for a table
// This function demonstrates how to use the WriteExamplesTemplate
// to generate table-specific queries by replacing placeholders
func GetWriteExampleQueries(tableName string) []string {
	// Replace {table} placeholder with actual table name
	content := strings.ReplaceAll(WriteExamplesTemplate, "{table}", tableName)

	// Split content by lines and remove "- " prefix
	lines := strings.Split(content, "\n")
	var queries []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = strings.TrimPrefix(line, "- ")
		queries = append(queries, line)
	}

	return queries
}

// RegisterPrompts registers all prompts with the MCP server
func RegisterPrompts(s *server.MCPServer) {
	// Register syntax guide prompt
	registerSyntaxGuidePrompt(s)

	// Register database description prompt
	registerDBDescriptionPrompt(s)

	// Register use case prompts
	registerUseCasePrompts(s)
}

// registerSyntaxGuidePrompt registers the parameterized syntax guide prompt
func registerSyntaxGuidePrompt(s *server.MCPServer) {
	// Create syntax guide prompt with parameter support
	syntaxGuidePrompt := mcp.NewPrompt("syntax_guide",
		mcp.WithPromptDescription("KWDB (KaiwuDB) syntax guide and examples. Optional parameters: 'database' and 'table' for table-specific guidance"),
		mcp.WithArgument("database", mcp.ArgumentDescription("Database name to provide specific table information")),
		mcp.WithArgument("table", mcp.ArgumentDescription("Table name to provide specific table schema and examples")),
	)

	// Add parameterized syntax guide prompt handler
	s.AddPrompt(syntaxGuidePrompt, func(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		arguments := request.Params.Arguments

		// Extract parameters - Arguments is already map[string]string
		database := arguments["database"]
		table := arguments["table"]

		// Base content
		baseContent := fmt.Sprintf(`You are a SQL expert specializing in KWDB (KaiwuDB). Help users understand the syntax and capabilities of the database.

%s`, SyntaxGuide)

		// Enhanced content for specific table
		tableContent := ""
		if table != "" {
			// Get table schema information
			if columns, err := db.GetTableColumnsWithContext(ctx, table); err == nil && len(columns) > 0 {
				tableContent += fmt.Sprintf("\n\n## Table Schema for '%s'\n", table)
				for _, col := range columns {
					columnName, _ := col["column_name"].(string)
					dataType, _ := col["data_type"].(string)
					isNullable, _ := col["is_nullable"].(string)
					columnDefault, _ := col["column_default"].(string)

					if columnName != "" && dataType != "" {
						tableContent += fmt.Sprintf("- **%s**: %s", columnName, dataType)
						if isNullable == "NO" {
							tableContent += " (NOT NULL)"
						}
						if columnDefault != "" {
							tableContent += fmt.Sprintf(" DEFAULT %s", columnDefault)
						}
						tableContent += "\n"
					}
				}

				// Add example queries for this table
				readExamples := GetReadExampleQueries(table)
				if len(readExamples) > 0 {
					tableContent += fmt.Sprintf("\n## Example Read Queries for '%s'\n", table)
					for _, example := range readExamples {
						if example != "" {
							tableContent += fmt.Sprintf("```sql\n%s\n```\n\n", example)
						}
					}
				}

				writeExamples := GetWriteExampleQueries(table)
				if len(writeExamples) > 0 {
					tableContent += fmt.Sprintf("\n## Example Write Queries for '%s'\n", table)
					for _, example := range writeExamples {
						if example != "" {
							tableContent += fmt.Sprintf("```sql\n%s\n```\n\n", example)
						}
					}
				}
			}
		}

		// Database information
		dbContent := ""
		if database != "" {
			if dbInfo, err := db.GetDatabaseInfoByName(database); err == nil {
				dbContent += fmt.Sprintf("\n\n## Database Information for '%s'\n", database)
				dbContent += fmt.Sprintf("- **Name**: %s\n", dbInfo.Name)
				dbContent += fmt.Sprintf("- **Version**: %s\n", dbInfo.Version)
				dbContent += fmt.Sprintf("- **Engine Type**: %s\n", dbInfo.EngineType)
				if dbInfo.Comment != "" {
					dbContent += fmt.Sprintf("- **Comment**: %s\n", dbInfo.Comment)
				}
				// Add properties if available
				for key, value := range dbInfo.Properties {
					dbContent += fmt.Sprintf("- **%s**: %v\n", key, value)
				}
			}
		}

		// Construct title
		title := "KWDB (KaiwuDB) Syntax Guide"
		if table != "" {
			title += fmt.Sprintf(" - Table: %s", table)
		}
		if database != "" {
			title += fmt.Sprintf(" - Database: %s", database)
		}

		return mcp.NewGetPromptResult(
			title,
			[]mcp.PromptMessage{
				mcp.NewPromptMessage(
					mcp.RoleUser,
					mcp.NewTextContent(baseContent+tableContent+dbContent),
				),
			},
		), nil
	})
}

// registerDBDescriptionPrompt registers the database description prompt
func registerDBDescriptionPrompt(s *server.MCPServer) {
	// Create database description prompt
	dbDescriptionPrompt := mcp.NewPrompt("db_description",
		mcp.WithPromptDescription("KWDB (KaiwuDB) database description and capabilities"),
	)

	// Add database description prompt handler
	s.AddPrompt(dbDescriptionPrompt, func(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return mcp.NewGetPromptResult(
			"KWDB (KaiwuDB) Database Description",
			[]mcp.PromptMessage{
				mcp.NewPromptMessage(
					mcp.RoleUser,
					mcp.NewTextContent("You are a database expert specializing in KWDB (KaiwuDB). Help users understand the capabilities and features of the database."),
				),
				mcp.NewPromptMessage(
					mcp.RoleAssistant,
					mcp.NewTextContent(DBDescription),
				),
			},
		), nil
	})
}

// registerUseCasePrompts registers all use case prompts
// When implementing auto-generation logic for prompts, follow this pattern:
// 1. Create a new Markdown template file in the docs/ directory
// 2. Add a variable to store the content
// 3. Load the file in the init() function
// 4. Create a registration function similar to the ones below
// 5. Add the registration function call to this function
func registerUseCasePrompts(s *server.MCPServer) {
	// Register cluster management prompt
	registerClusterManagementPrompt(s)

	// Register data migration prompt
	registerDataMigrationPrompt(s)

	// Register installation prompt
	registerInstallationPrompt(s)

	// Register performance tuning prompt
	registerPerformanceTuningPrompt(s)

	// Register troubleshooting prompt
	registerTroubleshootingPrompt(s)

	// Register backup and restore prompt
	registerBackupRestorePrompt(s)

	// Register DBA template prompt
	registerDBATemplatePrompt(s)

	// Note: To add new use case prompts, follow these steps:
	// 1. Create a new Markdown file in the docs/ directory, e.g., NewUseCase.md
	// 2. Add a new variable in prompts.go, e.g., var NewUseCaseGuide string
	// 3. Load the new Markdown file in the init() function
	// 4. Create a new registration function, e.g., registerNewUseCasePrompt(s *server.MCPServer)
	// 5. Call the new registration function in the registerUseCasePrompts function
	// 6. Update the README.md to document the new prompt
}

// registerClusterManagementPrompt registers the cluster management prompt
func registerClusterManagementPrompt(s *server.MCPServer) {
	prompt := mcp.NewPrompt("cluster_management",
		mcp.WithPromptDescription("KWDB Cluster Management Guide and Best Practices"),
	)

	s.AddPrompt(prompt, func(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return mcp.NewGetPromptResult(
			"KWDB Cluster Management Guide",
			[]mcp.PromptMessage{
				mcp.NewPromptMessage(
					mcp.RoleUser,
					mcp.NewTextContent("You are a KWDB cluster management expert. Help users understand and implement KWDB cluster management operations."),
				),
				mcp.NewPromptMessage(
					mcp.RoleAssistant,
					mcp.NewTextContent(ClusterManagementGuide),
				),
			},
		), nil
	})
}

// registerDataMigrationPrompt registers the data migration prompt
func registerDataMigrationPrompt(s *server.MCPServer) {
	prompt := mcp.NewPrompt("data_migration",
		mcp.WithPromptDescription("KWDB Data Migration Guide and Best Practices"),
	)

	s.AddPrompt(prompt, func(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return mcp.NewGetPromptResult(
			"KWDB Data Migration Guide",
			[]mcp.PromptMessage{
				mcp.NewPromptMessage(
					mcp.RoleUser,
					mcp.NewTextContent("You are a KWDB data migration expert. Help users understand and implement data migration operations."),
				),
				mcp.NewPromptMessage(
					mcp.RoleAssistant,
					mcp.NewTextContent(DataMigrationGuide),
				),
			},
		), nil
	})
}

// registerInstallationPrompt registers the installation prompt
func registerInstallationPrompt(s *server.MCPServer) {
	prompt := mcp.NewPrompt("installation",
		mcp.WithPromptDescription("KWDB Installation and Deployment Guide and Best Practices"),
	)

	s.AddPrompt(prompt, func(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return mcp.NewGetPromptResult(
			"KWDB Installation and Deployment Guide",
			[]mcp.PromptMessage{
				mcp.NewPromptMessage(
					mcp.RoleUser,
					mcp.NewTextContent("You are a KWDB installation and deployment expert. Help users understand and implement KWDB installation and deployment operations."),
				),
				mcp.NewPromptMessage(
					mcp.RoleAssistant,
					mcp.NewTextContent(InstallationGuide),
				),
			},
		), nil
	})
}

// registerPerformanceTuningPrompt registers the performance tuning prompt
func registerPerformanceTuningPrompt(s *server.MCPServer) {
	prompt := mcp.NewPrompt("performance_tuning",
		mcp.WithPromptDescription("KWDB Performance Tuning Guide and Best Practices"),
	)

	s.AddPrompt(prompt, func(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return mcp.NewGetPromptResult(
			"KWDB Performance Tuning Guide",
			[]mcp.PromptMessage{
				mcp.NewPromptMessage(
					mcp.RoleUser,
					mcp.NewTextContent("You are a KWDB performance tuning expert. Help users understand and implement performance optimization operations."),
				),
				mcp.NewPromptMessage(
					mcp.RoleAssistant,
					mcp.NewTextContent(PerformanceTuningGuide),
				),
			},
		), nil
	})
}

// registerTroubleshootingPrompt registers the troubleshooting prompt
func registerTroubleshootingPrompt(s *server.MCPServer) {
	prompt := mcp.NewPrompt("troubleshooting",
		mcp.WithPromptDescription("KWDB Troubleshooting Guide and Best Practices"),
	)

	s.AddPrompt(prompt, func(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return mcp.NewGetPromptResult(
			"KWDB Troubleshooting Guide",
			[]mcp.PromptMessage{
				mcp.NewPromptMessage(
					mcp.RoleUser,
					mcp.NewTextContent("You are a KWDB troubleshooting expert. Help users diagnose and resolve KWDB issues."),
				),
				mcp.NewPromptMessage(
					mcp.RoleAssistant,
					mcp.NewTextContent(TroubleshootingGuide),
				),
			},
		), nil
	})
}

// registerBackupRestorePrompt registers the backup and restore prompt
// This is an example of how to add a new prompt using the template pattern
func registerBackupRestorePrompt(s *server.MCPServer) {
	prompt := mcp.NewPrompt("backup_restore",
		mcp.WithPromptDescription("KWDB Backup and Restore Guide and Best Practices"),
	)

	s.AddPrompt(prompt, func(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return mcp.NewGetPromptResult(
			"KWDB Backup and Restore Guide",
			[]mcp.PromptMessage{
				mcp.NewPromptMessage(
					mcp.RoleUser,
					mcp.NewTextContent("You are a KWDB backup and restore expert. Help users understand and implement backup and restore operations."),
				),
				mcp.NewPromptMessage(
					mcp.RoleAssistant,
					mcp.NewTextContent(BackupRestoreGuide),
				),
			},
		), nil
	})
}

// registerDBATemplatePrompt registers the DBA template prompt
// This is an example of how to add a new prompt using the template pattern
func registerDBATemplatePrompt(s *server.MCPServer) {
	prompt := mcp.NewPrompt("dba_template",
		mcp.WithPromptDescription("KWDB Database Administration Template and Best Practices"),
	)

	s.AddPrompt(prompt, func(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return mcp.NewGetPromptResult(
			"KWDB Database Administration Template",
			[]mcp.PromptMessage{
				mcp.NewPromptMessage(
					mcp.RoleUser,
					mcp.NewTextContent("You are a KWDB database administration expert. Help users understand and implement database administration operations."),
				),
				mcp.NewPromptMessage(
					mcp.RoleAssistant,
					mcp.NewTextContent(DBATemplate),
				),
			},
		), nil
	})
}
