package Chat

import (
	"encoding/json"
	"github.com/anyscapelabs/argus/Protocol"
	"github.com/anyscapelabs/argus/Services/Permission"
)

var terminalToolDef = Protocol.ToolDef{
	Name: Permission.TerminalTool,
	Description: "Run one shell command on the user's machine using /bin/sh. " +
		"Returns combined stdout/stderr and the exit code. " +
		"The user must approve terminal access through the app before the first command runs; " +
		"if they decline you receive an error result and should continue without executing commands.",
	Schema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"command": {
				"type": "string",
				"description": "The shell command to execute"
			},
			"timeout_seconds": {
				"type": "integer",
				"description": "Optional timeout in seconds, between 1 and 300. Default 60."
			}
		},
		"required": ["command"]
	}`),
}

var readFileToolDef = Protocol.ToolDef{
	Name: "read_file",
	Description: "Read one text file, paged by lines. Returns the content plus the total line count " +
		"and whether the page was cut, so you can page with offset to see the rest. " +
		"Binary files are refused. Use it to check facts on disk, read what a routine wrote, " +
		"or inspect a document the user mentions. Needs the user's file permission first.",
	Schema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {
				"type": "string",
				"description": "File path, absolute or starting with ~"
			},
			"offset": {
				"type": "integer",
				"description": "Optional 1-based line to start from. Default 1."
			},
			"limit": {
				"type": "integer",
				"description": "Optional max lines to return, between 1 and 2000. Default 2000."
			}
		},
		"required": ["path"]
	}`),
}

var writeFileToolDef = Protocol.ToolDef{
	Name: "write_file",
	Description: "Create, replace or append to one text file; parent folders are created as needed. " +
		"An existing file is never touched without overwrite=true or append=true — say what you intend. " +
		"Use it to save a report, a note, or anything the user asked to be written down. " +
		"Needs the user's file permission first.",
	Schema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {
				"type": "string",
				"description": "File path, absolute or starting with ~"
			},
			"content": {
				"type": "string",
				"description": "The full text to write, up to 1 MB"
			},
			"overwrite": {
				"type": "boolean",
				"description": "Replace the whole file if it already exists"
			},
			"append": {
				"type": "boolean",
				"description": "Add to the end of an existing file instead of replacing it"
			}
		},
		"required": ["path", "content"]
	}`),
}

var grepFilesToolDef = Protocol.ToolDef{
	Name: "grep_files",
	Description: "Search files under one folder for a regular expression and get each matching line " +
		"with its file and line number, dependencies and binaries skipped. " +
		"Use instead of read-then-scan when the answer could be anywhere in the tree, " +
		"e.g. finding where a project defines a function. Needs the user's file permission first.",
	Schema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"pattern": {
				"type": "string",
				"description": "Go regular expression"
			},
			"include": {
				"type": "string",
				"description": "Optional filename glob to filter, e.g. *.go or *.ts"
			},
			"path": {
				"type": "string",
				"description": "Folder to search, absolute or starting with ~"
			}
		},
		"required": ["pattern", "path"]
	}`),
}
