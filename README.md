# LinkLog: AI-Powered Networking CRM

LinkLog is a Telegram-based personal networking manager designed to help you maintain and grow your professional and personal connections. It uses LLMs to process your interactions and maintains a structured knowledge base in [Outline](https://www.getoutline.com/).

## 🚀 The Vision
In a world of "out of sight, out of mind," LinkLog acts as your digital memory and proactive networking assistant. Instead of manual spreadsheet entries, you simply "talk" to your networking log. The bot handles the categorization, summarization, and scheduling of your next interactions.

## ✨ Key Features
- **🤖 LLM-Driven Intelligence**: Uses OpenAI-compatible APIs to analyze your interaction logs, update contact personas, and suggest follow-up topics.
- **📁 Outline as Persistence**: Every contact and interaction is stored as a document in Outline, providing a beautiful, searchable, and collaborative interface for your networking database.
- **🗂️ Smart Categorization**: Automatically organizes contacts into hierarchical structures (e.g., Colleagues, Friends, Industry Clubs).
- **📅 Proactive Reminders**: Notifies you when it's time to reach out to someone based on custom intervals or LLM suggestions.
- **🔄 Interactive Loops**:
    - Receive a reminder.
    - Snooze or mark as "Contacted".
    - Provide a quick voice-to-text or typed summary of the interaction.
    - Watch as the bot updates the contact's history and calculates the next touchpoint.

## 🏗️ Architecture
- **Language**: Go (Golang)
- **Interface**: Telegram Bot API
- **Brain**: OpenAI-compatible LLM (GPT-4, Claude, or local via Ollama)
- **Database/Storage**: Outline API
- **Scheduling**: Internal cron-based reminder system

## 📋 Data Structure in Outline
LinkLog creates a "Networking" collection with the following hierarchy:
- **Networking (Collection)**
    - **Category (Folder: e.g., "Tech Clubs")**
        - **Contact Name (Document)**
            - Profile summary (Bio, interests, context)
            - **Interaction Log (Sub-documents or nested lists)**
                - Date: [Topic] - [Outcome] - [Next Step]

## 🛠️ Configuration
The bot requires the following environment variables:
- `TELEGRAM_BOT_TOKEN`: Your bot token from @BotFather.
- `OUTLINE_API_KEY`: API key from your Outline instance.
- `OUTLINE_BASE_URL`: The URL of your Outline instance.
- `LLM_API_KEY`: API key for OpenAI or compatible provider.
- `LLM_BASE_URL`: Endpoint for the LLM API.
- `LLM_MODEL`: The specific model to use (e.g., `gpt-4o`).

## 🚶 Workflow
1. **Initial Input**: Tell the bot: "Met Alex from Stripe today. We talked about Go microservices. He likes kite-surfing. We should sync again in a month."
2. **Processing**: The LLM creates/updates the "Alex" document in the "Work" category in Outline.
3. **Reminder**: One month later, the bot sends: "Time to catch up with Alex. Last time: Go microservices. Suggestion: Ask how the kite-surfing trip went."
4. **Update**: You click "Done", type "Great sync, moving to a bi-weekly cadence", and the bot updates the records accordingly.

---
*Built with ❤️ for better connections.*
