package agent

// DescriptionFieldSpec is the standard JSON-schema entry for the `description`
// field every approval-required tool must include alongside its native args.
// It surfaces on approval cards in the user's UI language so non-technical
// users can read what the agent is about to do — instead of seeing a raw JSON
// blob like {"path":"/Users/.../file.md"} or a long shell command.
//
// Wired by per-tool Info().Parameters.properties. The matching tool MUST
// declare "description" in Required so the model is forced to populate it.
//
// Bash has its own bespoke schema (PR 4 wrote one before this helper existed)
// and intentionally does NOT use this spec — that schema is more detailed
// and changing it would invalidate the prompt cache. Future schema cleanup
// can converge bash onto this helper if cache impact is acceptable.
var DescriptionFieldSpec = map[string]any{
	"type": "string",
	"description": "REQUIRED. A short (5-15 word) natural-language summary of WHAT this call does, " +
		"written in the user's UI language (中文 / English / etc.). " +
		"Describe the user-facing INTENT, not the API/path/syntax. " +
		"The end user — often non-technical — sees this, not the args, on the approval prompt. " +
		"Examples: '查看 ui-components 引用', 'Save login page HTML', '生成头像图片', 'List Downloads folder'. " +
		"Do NOT paraphrase the args literally; describe the goal in plain language.",
}

// DescriptionGuidance is the standard instructions snippet appended to every
// approval-required tool's top-level Description (the text shown to the
// model, NOT the user) so the model consistently produces a usable summary
// across tools.
const DescriptionGuidance = `

IMPORTANT: ALWAYS include a "description" field — a 5-15 word natural-language summary of the user-facing intent, in the user's UI language. The end user (often non-technical) sees the description on the approval prompt, NOT the raw arguments. Describe the goal, not the syntax.`
