package agent

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// safeToolNameRE validates tool names for prompt embedding — prevents injection.
var safeToolNameRE = regexp.MustCompile(`^[a-zA-Z0-9_\-]{1,64}$`)

const maxSkillDraftBytes = 8192

// ToolSpanSample is a recent tool call sample for LLM context.
type ToolSpanSample struct {
	Input  string // truncated tool input (e.g. command, query)
	Output string // truncated tool output
}

// ToolSpanSampler queries recent tool call samples from tracing spans.
type ToolSpanSampler interface {
	SampleToolSpans(ctx context.Context, agentID uuid.UUID, toolName string, limit int) ([]ToolSpanSample, error)
}

// GenerateSkillDraftLLM uses an LLM to generate a meaningful SKILL.md from tool usage metrics.
// Falls back to the static template if the LLM call fails or provider is nil.
// When sampler is provided, includes recent tool call samples for richer context.
func GenerateSkillDraftLLM(ctx context.Context, provider providers.Provider, model string, toolName string, agg store.ToolAggregate, sampler ToolSpanSampler, agentID uuid.UUID) string {
	if provider == nil {
		return GenerateSkillDraft(toolName, agg.CallCount, agg.SuccessRate)
	}
	// Sanitize tool name to prevent prompt injection
	if !safeToolNameRE.MatchString(toolName) {
		slog.Warn("evolution.skill_draft_llm: unsafe tool name, using static template", "tool", toolName)
		return GenerateSkillDraft(toolName, agg.CallCount, agg.SuccessRate)
	}

	systemPrompt := `You are generating a SKILL.md file for an AI agent based on tool usage metrics.
Output ONLY the SKILL.md content with frontmatter. No explanation, no code fences.

Format:
---
name: <tool>-patterns
description: "<one-line description>"
---

# <Tool> Usage Patterns

## When to Use
<2-3 bullet points>

## Instructions
<3-5 specific instructions>

## Constraints
<2-3 guardrails>`

	var samplesText string
	if sampler != nil {
		if samples, err := sampler.SampleToolSpans(ctx, agentID, toolName, 5); err == nil && len(samples) > 0 {
			var sb strings.Builder
			sb.WriteString("\n\nRecent usage samples:\n")
			for i, s := range samples {
				sb.WriteString(fmt.Sprintf("  %d. Input: %s\n     Output: %s\n", i+1, s.Input, s.Output))
			}
			samplesText = sb.String()
		}
	}

	userMsg := fmt.Sprintf(`Tool: %s
Calls/week: %d
Success rate: %.0f%%
Avg duration: %.0fms%s

Generate a practical SKILL.md that captures when and how to use this tool effectively based on the usage data and samples above.`,
		toolName, agg.CallCount, agg.SuccessRate*100, agg.AvgDurationMs, samplesText)

	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if model == "" {
		model = provider.DefaultModel()
	}
	if model == "" {
		slog.Debug("evolution.skill_draft_llm_skipped", "tool", toolName, "reason", "no model configured")
		return GenerateSkillDraft(toolName, agg.CallCount, agg.SuccessRate)
	}

	resp, err := provider.Chat(callCtx, providers.ChatRequest{
		Messages: []providers.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userMsg},
		},
		Model: model,
		Options: map[string]any{
			"max_tokens":  500,
			"temperature": 0.3,
		},
	})
	if err != nil {
		slog.Warn("evolution.skill_draft_llm_failed", "tool", toolName, "error", err)
		return GenerateSkillDraft(toolName, agg.CallCount, agg.SuccessRate)
	}

	content := strings.TrimSpace(resp.Content)
	if len(content) > maxSkillDraftBytes {
		slog.Warn("evolution.skill_draft_llm: response too large", "tool", toolName, "size", len(content))
		return GenerateSkillDraft(toolName, agg.CallCount, agg.SuccessRate)
	}
	// Validate frontmatter is parseable and has a valid name
	name, _, _, _ := skills.ParseSkillFrontmatter(content)
	if name == "" {
		slog.Warn("evolution.skill_draft_llm: invalid frontmatter", "tool", toolName)
		return GenerateSkillDraft(toolName, agg.CallCount, agg.SuccessRate)
	}
	return content
}

// GenerateSkillDraft creates a SKILL.md template from repeated tool usage data.
// Produces a skeleton that admin can edit before activation via evolution approval.
func GenerateSkillDraft(toolName string, callCount int, successRate float64) string {
	return fmt.Sprintf(`---
name: %s-patterns
description: Skill auto-generated from repeated %s tool usage (%d calls/week, %.0f%% success)
---

# %s Usage Patterns

Auto-generated from tool metrics. Edit before activating.

## When to Use

Describe scenarios where this tool pattern should be applied automatically.

## Instructions

Provide specific instructions for using %s effectively based on observed patterns.

## Constraints

List any constraints or guardrails for this tool usage.
`, toolName, toolName, callCount, successRate*100, toolName, toolName)
}
