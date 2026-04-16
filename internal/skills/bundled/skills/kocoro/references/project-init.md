# Project Init

## What is this?

Project init creates a `.shannon/` directory inside a specific project folder, enabling project-level configuration. Once initialized, you can put project-specific config, instructions, and rules in that directory — and those settings will apply whenever an agent works in that project, layered on top of global settings. The operation is safe to run multiple times.

## API Endpoints

### Initialize a project
- Method: POST
- Path: /project/init
- Body: `{"cwd": "/path/to/your/project", "instructions": "This project is a React app. Always use TypeScript."}`
- Response: `{"cwd": "/path/to/your/project", "created": [".shannon/", ".shannon/instructions.md"], "existed": [".shannon/config.yaml"]}`
- Notes: `instructions` is optional. If `.shannon/` already exists, only missing files are created — nothing is overwritten. The `created` and `existed` arrays tell you exactly what happened.

## What Gets Created

| Path | Purpose | Created if |
|------|---------|-----------|
| `.shannon/` | Config directory root | Always (if not present) |
| `.shannon/config.yaml` | Project-level config overrides | Never automatically — create manually when needed |
| `.shannon/instructions.md` | Project-specific agent instructions | Only if `instructions` body field is provided |
| `.shannon/config.local.yaml` | Local overrides (gitignored) | Never automatically — create manually when needed |

## Common Scenarios

### "Set up this project for Shannon"
1. Confirm the project path (e.g., `/Users/me/projects/myapp`)
2. POST /project/init with `{"cwd": "/Users/me/projects/myapp"}`
3. Check response: `created` shows what was made, `existed` shows what was already there
4. Optionally add instructions: POST /project/init with `{"cwd": "/Users/me/projects/myapp", "instructions": "This is a Go CLI tool. Use gofmt conventions."}`

### "Project already has .shannon — will it break?"
No. The endpoint is idempotent. If `.shannon/` already exists, only missing subdirectories or files are created. Existing files are never overwritten. The response shows everything in `existed` so you can see what was preserved.

### "Add project-specific instructions after init"
1. PUT /instructions?project=/path/to/project (if supported), or
2. Edit `.shannon/instructions.md` directly in your project, then POST /config/reload

### "Configure the project to use a different model"
1. After init, create or edit `.shannon/config.yaml` in the project with:
   ```yaml
   agent:
     model: claude-opus-4-5
   ```
2. POST /config/reload

## Safety Notes

- **Confirm the path first**: Always verify `cwd` is the correct project directory. Project config at the wrong path has no effect (Shannon looks for `.shannon/` relative to the working directory at conversation start).
- **Never overwrites**: Existing files in `.shannon/` are never modified by project init. You must edit them manually.
- **Version control**: The `.shannon/` directory is typically committed to version control so the whole team shares project instructions and config. Exception: `.shannon/config.local.yaml` should be gitignored since it contains machine-specific or secret settings.
- **Idempotent**: Safe to run multiple times. Running init on an already-initialized project just returns what existed.
- **Effect timing**: New project instructions take effect on the next conversation. The current in-progress conversation is not affected.
