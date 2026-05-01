---
# Implement LinkLog Telegram Bot

## Overview
Implement the LinkLog Telegram bot as described in the README, combining the Model Context Protocol (MCP) for LLM autonomy with a direct Outline REST API client for bot-managed state. The bot will be written in Go using module path `github.com/korjavin/linklog`, `gopkg.in/telebot.v3` for Telegram and `github.com/sashabaranov/go-openai` for LLM processing. The LLM agent will use the Outline MCP server to autonomously manage documents and folders based on user input. Additionally, the bot itself will use a direct Outline REST client to maintain a "schedule table" document. After the agent processes an interaction, the bot will ask the agent for a suggested follow-up date (defaulting to ~1 week) and update the schedule table. A scheduler will run every 2 hours to read this table and send proactive notifications. A `Dockerfile` and `docker-compose.yml` will be provided for deployment.

## Context
- Files involved: `cmd/linklog/main.go`, `internal/config/config.go`, `internal/mcp/client.go`, `internal/outline/client.go`, `internal/llm/service.go`, `internal/bot/bot.go`, `internal/scheduler/scheduler.go`, `Dockerfile`, `docker-compose.yml`
- Dependencies: `gopkg.in/telebot.v3`, `github.com/sashabaranov/go-openai`, `github.com/joho/godotenv`, `github.com/robfig/cron/v3`, a Go MCP client library

## Development Approach
- **Testing approach**: Regular (code first, then tests) no unit tests, only integration tests if appropriate
- Complete each task fully before moving to the next
- **CRITICAL: all tests must pass before starting next task**

## Implementation Steps

### Task 1: Project Setup & Docker Configuration

**Files:**
- Modify: `go.mod` (new)
- Create: `cmd/linklog/main.go`, `internal/config/config.go`, `Dockerfile`, `docker-compose.yml`, `.env.example`

- [x] Initialize Go module (`go mod init github.com/korjavin/linklog`)
- [x] Create `.env.example` with required variables (`TELEGRAM_BOT_TOKEN`, `OUTLINE_API_KEY`, `OUTLINE_BASE_URL`, `OUTLINE_COLLECTION_ID`, `LLM_API_KEY`, `LLM_BASE_URL`, `LLM_MODEL`, `SCHEDULE_DOC_ID`)
- [x] Implement `internal/config/config.go` to parse environment variables using `godotenv`
- [x] Create a skeleton `cmd/linklog/main.go`
- [x] Create `Dockerfile` and `docker-compose.yml` (including MCP server if necessary)
- [x] Run basic build to verify compilation

### Task 2: MCP Client Integration (for LLM Agent)

**Files:**
- Create: `internal/mcp/client.go`, `internal/mcp/client_test.go`

- [x] Integrate a Go MCP client (e.g., `github.com/mark3labs/mcp-go`)
- [x] Configure the client to connect to the Outline MCP server
- [x] Implement tool fetching and mapping into OpenAI function definitions
- [x] Write integration tests for MCP tool discovery
- [x] Run project test suite - must pass before task 3

### Task 3: Outline REST API Client (for Bot State)

**Files:**
- Create: `internal/outline/client.go`, `internal/outline/client_test.go`

- [x] Implement basic HTTP client setup for Outline's REST API using `OUTLINE_API_KEY`
- [x] Implement methods to read and update a specific "schedule table" document (by ID or path)
- [x] Implement a helper to parse the schedule table into a Go struct (e.g., mapping user/contact to a follow-up date) and to serialize it back to markdown
- [x] Write integration tests for the Outline REST client
- [x] Run project test suite - must pass before task 4

### Task 4: LLM Agent Integration

**Files:**
- Create: `internal/llm/service.go`, `internal/llm/service_test.go`

- [ ] Integrate `github.com/sashabaranov/go-openai`
- [ ] Define the system prompt for the MCP-enabled agent to manage Outline content (passing `OUTLINE_COLLECTION_ID` to restrict scope/context)
- [ ] Implement the tool-calling loop: executing MCP tools autonomously
- [ ] Implement a specific prompt/function to ask the LLM for a "suggested date of next contact" after it finishes processing a document
- [ ] Write integration tests for the LLM service
- [ ] Run project test suite - must pass before task 5

### Task 5: Telegram Bot Foundation & Flow

**Files:**
- Create: `internal/bot/bot.go`

- [ ] Set up `gopkg.in/telebot.v3` in `internal/bot/bot.go`
- [ ] Implement text handler: receive input and pass it to the LLM agent (which updates Outline via MCP)
- [ ] After LLM processing, extract the "suggested date of next contact" from the LLM (if missing, default to +1 week)
- [ ] Use the Outline REST client to append or update this contact's date in the "schedule table" document
- [ ] Wire up the bot in `cmd/linklog/main.go`
- [ ] Run project test suite - must pass before task 6

### Task 6: Reminder & Scheduler System

**Files:**
- Create: `internal/scheduler/scheduler.go`

- [ ] Integrate `github.com/robfig/cron/v3`
- [ ] Implement a job that runs every 2 hours
- [ ] The job uses the Outline REST client to read the "schedule table" document, parse the dates, and identify due follow-ups
- [ ] Send Telegram notifications for any due interactions
- [ ] Add the scheduler initialization to `cmd/linklog/main.go`
- [ ] Run project test suite - must pass before task 7

### Task 7: Verify acceptance criteria

- [ ] run full test suite (`go test ./...`)
- [ ] run linter (`golangci-lint run`)

### Task 8: Update documentation

- [ ] update README.md if user-facing changes
- [ ] update CLAUDE.md if internal patterns changed
- [ ] move this plan to `docs/plans/completed/`
---