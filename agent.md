# Agent Context: LinkLog

LinkLog is an AI-powered networking CRM that uses Telegram as an interface, Outline as a knowledge base, and LLMs (via MCP) for autonomous data management.

## 🏗️ Architecture & Stack
- **Language**: Go 1.26.2
- **Interface**: Telegram Bot (`gopkg.in/telebot.v3`)
- **Intelligence**: OpenAI-compatible LLM (`github.com/sashabaranov/go-openai`)
- **Knowledge Base**: Outline ([getoutline.com](https://www.getoutline.com/))
    - **LLM Autonomy**: Model Context Protocol (MCP) using `github.com/mark3labs/mcp-go` to connect the LLM to Outline tools.
    - **Bot State**: Direct REST API client for managing the "schedule table" document.
- **Scheduling**: `github.com/robfig/cron/v3` for periodic reminder checks.

## 📁 Project Structure
- `cmd/linklog/`: Main entry point and service orchestration.
- `internal/bot/`: Telegram bot logic, message handling, and formatting (respecting 4096 char limits).
- `internal/config/`: Environment variable parsing and configuration.
- `internal/llm/`: LLM service integration, system prompts, and MCP tool execution loop.
- `internal/mcp/`: Outline MCP client for bridging LLM and Outline tools.
- `internal/outline/`: REST API client for Outline, including GFM table parsing/serialization for the schedule.
- `internal/scheduler/`: Cron-based jobs for sending proactive Telegram reminders.

## 🛠️ Development & Coding Standards
- **Testing**: Use `github.com/stretchr/testify` for assertions.
    - Run tests: `go test ./...`
- **Error Handling**: Prefer error wrapping with `%w`.
- **Concurrency**: Use `context.Context` throughout the service layers.
- **Formatting**: Adhere to standard `go fmt` and `golangci-lint`.
- **Telegram Limits**: Always use `splitForTelegram` or `truncateToUTF8Bytes` (in `internal/bot`) when sending long content.
- **Outline Tables**: Use `ParseScheduleTable` and `SerializeScheduleTable` (in `internal/outline`) for manipulating the follow-up schedule.

## 🔄 Core Workflows
1. **Interaction Processing**:
   - User message -> `bot` -> `llm`.
   - `llm` uses `mcp` tools to create/update documents in Outline collection.
   - `llm` suggests a next contact date.
   - `bot` updates the "schedule table" document in Outline via `outline` REST client.
2. **Reminders**:
   - `scheduler` runs every 2 hours.
   - Reads "schedule table" from Outline via `outline` REST client.
   - Sends Telegram notifications via `bot` for contacts due for follow-up.

## ⚙️ Environment Setup
Required variables (see `.env.example`):
- `TELEGRAM_BOT_TOKEN`, `TELEGRAM_ADMIN_CHAT_ID`
- `OUTLINE_API_KEY`, `OUTLINE_BASE_URL`, `OUTLINE_COLLECTION_ID`, `SCHEDULE_DOC_ID`
- `LLM_API_KEY`, `LLM_BASE_URL`, `LLM_MODEL`

## 📝 Recent Plans
Check `docs/plans/` for ongoing and completed project milestones.
