# Hartley - AI-Powered Action Server

## Overview
Hartley is an AI-driven command execution server that listens for API requests and executes predefined actions. If an action isn't explicitly defined, Hartley queries Google's Gemini API to generate Python code dynamically, executes it, and returns the results.

This project is designed for flexibility and extensibility, allowing users to add new actions easily while ensuring all executions and logs are stored in an SQLite database.

## Features
- **API-Driven**: Receives requests over HTTP and executes actions.
- **Predefined Actions**: Supports predefined JSON-configured commands.
- **AI Code Generation**: Uses Google's Gemini API to generate Python scripts dynamically.
- **Database Logging**: Stores API requests, responses, and execution logs in SQLite.
- **Extensible & Customizable**: Users can modify the action list, API configurations, and logging mechanism.
- **Command Line Compatible**: Can be accessed from any OS on the network using standard API tools like `curl`.

## Project Structure
```
Hartley/
├── go.mod               # Go module file
├── hartley-main.go      # Main server file
├── config/
│   └── config.json      # Configuration settings (server port, API keys, etc.)
├── actions/
│   └── actions.json     # List of predefined actions
├── db/
│   └── hartley.db       # SQLite database (created at runtime)
├── logs/
│   └── hartley.log      # Optional log file (if enabled)
├── python/
│   └── action_runner.py # Python script for executing predefined actions
└── cli/
    └── hartley_cli.py   # CLI tool for interacting with the API
```

## Installation & Setup

### 1. Install Dependencies
Make sure you have the following installed:
- **Go 1.18+**: Install from [golang.org](https://golang.org/doc/install).
- **Python 3**: Install from [python.org](https://www.python.org/downloads/).
- **SQLite**: Installed by default on most systems, otherwise install using:
  - macOS: `brew install sqlite`
  - Linux: `sudo apt install sqlite3`
  - Windows: Install SQLite manually from [sqlite.org](https://www.sqlite.org/download.html).

### 2. Clone the Repository
```sh
git clone https://github.com/yourusername/hartley.git
cd hartley
```

### 3. Set Up Configuration
Create `config/config.json` to include your Google Gemini API key and configure the server port:
```json
{
  "server_port": 8080,
  "gemini_api_key": "YOUR_API_KEY",
  "gemini_endpoint": "https://generativelanguage.googleapis.com/v1beta/models/gemini-1.5-flash:generateContent"
}
```

### 4. Build and Run the Server
```sh
go mod tidy
go build -o hartley-main hartley-main.go
./hartley-main
```
The server will start listening on port `8080` (or the port set in `config.json`).

## Usage

### Sending Requests
You can send API requests using `curl`, Postman, or the included CLI tool.

#### Example 1: Executing a Predefined Action
If `actions.json` includes:
```json
[
  {
    "name": "run_terminal_command",
    "description": "Executes a terminal command",
    "script": "python/action_runner.py",
    "function": "run_terminal_command"
  }
]
```
Then, execute it using:
```sh
curl -X POST http://localhost:8080/api/action \
  -H "Content-Type: application/json" \
  -d '{"action": "run_terminal_command", "params": {"command": "echo Hello, Hartley!"}}'
```
Expected response:
```json
{
  "stdout": "Hello, Hartley!\n",
  "stderr": "",
  "returncode": 0
}
```

#### Example 2: Requesting an AI-Generated Action
If the requested action is **not** in `actions.json`, Hartley will query Gemini to generate Python code.
```sh
curl -X POST http://localhost:8080/api/action \
  -H "Content-Type: application/json" \
  -d '{"action": "explain how AI works", "params": {}}'
```
Hartley will:
1. Query Gemini to generate a Python script that prints an explanation.
2. Execute the generated script.
3. Return the output in JSON format.

Example response:
```json
{
  "result": "AI works by processing data and learning patterns."
}
```

### Using the CLI Tool
```sh
./cli/hartley_cli.py --action "run_terminal_command" --params '{"command": "ls"}'
```

## Logging & Debugging
Hartley logs execution details in the console and stores logs in SQLite. To view logged actions:
```sh
sqlite3 db/hartley.db "SELECT * FROM logs;"
```
To monitor Gemini responses and Python execution:
```sh
tail -f logs/hartley.log
```

## Extending the Project
### Adding a New Predefined Action
Modify `actions.json` and add an entry:
```json
{
  "name": "get_current_time",
  "description": "Gets the system time",
  "script": "python/action_runner.py",
  "function": "get_current_time"
}
```
Then, implement the function in `python/action_runner.py`:
```python
def get_current_time(params):
    import datetime
    return {"result": datetime.datetime.now().isoformat()}
```
Restart the server to apply changes.

## Security Considerations
- **Executing AI-generated code is risky**: Ensure Gemini’s generated code is safe before execution.
- **API Key Protection**: Store API keys securely, not in public repositories.
- **Access Control**: Implement authentication if exposing Hartley over a network.

## Future Enhancements
- **Containerization**: Dockerize for easier deployment.
- **Web UI**: Build a dashboard for managing actions and viewing logs.
- **Improved AI Filtering**: Ensure only safe Python code is executed.

## Contributors
- **Micah Crandell** - Original Developer

## License
MIT License. Free to use and modify.

## Contact
For questions or improvements, submit an issue or pull request on GitHub.

