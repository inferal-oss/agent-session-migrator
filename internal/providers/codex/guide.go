package codex

import _ "embed"

//go:embed runner.md
var executionGuide string

func (*Provider) ExecutionGuide() string { return executionGuide }
