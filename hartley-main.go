package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	// a pure-Go SQLite driver.
	_ "modernc.org/sqlite"
)

// ----------------------
// Configuration and Action Types
// ----------------------

// Config holds configuration for the server and Gemini API.
type Config struct {
	ServerPort     int    `json:"server_port"`
	GeminiAPIKey   string `json:"gemini_api_key"`
	GeminiEndpoint string `json:"gemini_endpoint"`
}

// Action represents a locally defined action.
type Action struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Script      string `json:"script"`
	Function    string `json:"function"`
}

// ActionRequest is the payload received by the API.
type ActionRequest struct {
	Action string                 `json:"action"`
	Params map[string]interface{} `json:"params"`
}

// ----------------------
// Gemini Response Structures
// ----------------------

type GeminiPart struct {
	Text string `json:"text"`
}

type GeminiContent struct {
	Parts []GeminiPart `json:"parts"`
	Role  string       `json:"role"`
}

type GeminiCandidate struct {
	Content      GeminiContent `json:"content"`
	FinishReason string        `json:"finishReason"`
}

type GeminiResponse struct {
	Candidates []GeminiCandidate `json:"candidates"`
	// Other fields are omitted for brevity.
}

// ----------------------
// Global Variables
// ----------------------

var (
	config  Config
	actions []Action
	dbConn  *sql.DB
)

// ----------------------
// Main Function
// ----------------------

func main() {
	// Load configuration.
	confData, err := ioutil.ReadFile("config/config.json")
	if err != nil {
		log.Fatalf("Error reading config: %v", err)
	}
	if err := json.Unmarshal(confData, &config); err != nil {
		log.Fatalf("Error parsing config: %v", err)
	}

	// Load actions configuration.
	actionsData, err := ioutil.ReadFile("actions/actions.json")
	if err != nil {
		log.Fatalf("Error reading actions config: %v", err)
	}
	if err := json.Unmarshal(actionsData, &actions); err != nil {
		log.Fatalf("Error parsing actions: %v", err)
	}

	// Initialize SQLite database using the modernc.org/sqlite driver.
	dbConn, err = sql.Open("sqlite", "./db/hartley.db")
	if err != nil {
		log.Fatalf("Error opening database: %v", err)
	}
	defer dbConn.Close()

	// Create logs table if not exists.
	_, err = dbConn.Exec(`
		CREATE TABLE IF NOT EXISTS logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp DATETIME,
			action TEXT,
			request TEXT,
			response TEXT
		)
	`)
	if err != nil {
		log.Fatalf("Error creating logs table: %v", err)
	}

	// Set up HTTP API.
	http.HandleFunc("/api/action", actionHandler)

	addr := fmt.Sprintf(":%d", config.ServerPort)
	log.Printf("Hartley server starting on %s...", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Error starting server: %v", err)
	}
}

// ----------------------
// Utility Functions
// ----------------------

// runPython attempts to run the Python script using "python3" first.
// If that fails, it falls back to "python", and then "py" (the Windows launcher).
func runPython(args ...string) ([]byte, error) {
	// Try python3 first.
	out, err := exec.Command("python3", args...).CombinedOutput()
	if err != nil && (errors.Is(err, exec.ErrNotFound) || strings.Contains(string(out), "Python was not found")) {
		log.Printf("python3 not found, falling back to python")
		out, err = exec.Command("python", args...).CombinedOutput()
		// If python still isn't found, try the Windows launcher.
		if err != nil && (errors.Is(err, exec.ErrNotFound) || strings.Contains(string(out), "Python was not found")) {
			log.Printf("python not found, falling back to py")
			out, err = exec.Command("py", args...).CombinedOutput()
		}
	}
	return out, err
}

// actionHandler processes incoming action requests.
func actionHandler(w http.ResponseWriter, r *http.Request) {
	// Ensure only POST requests are accepted.
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		jsonResponse(w, map[string]string{"error": "Method not allowed"})
		return
	}

	// Decode the incoming JSON request.
	var req ActionRequest
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		jsonResponse(w, map[string]string{"error": "Invalid JSON"})
		return
	}

	var resp map[string]interface{}

	// Check for two–letter command prefixes (e.g., "py", "sh").
	prefix, payload := parsePrefix(req.Action)
	if prefix == "py" {
		// Process Python code generation command.
		resp = handlePythonCommand(payload)
	} else if prefix == "sh" {
		// Process shell command execution.
		resp = handleShellCommand(payload)
	} else {
		// No prefix: check if the action exists in local actions.
		actionFound := false
		for _, act := range actions {
			if act.Name == req.Action {
				actionFound = true
				paramsJSON, err := json.Marshal(req.Params)
				if err != nil {
					resp = map[string]interface{}{"error": "Error marshalling parameters"}
					break
				}
				// Execute the defined Python script using our python fallback.
				output, err := runPython(act.Script, act.Function, string(paramsJSON))
				if err != nil {
					resp = map[string]interface{}{
						"error":  fmt.Sprintf("Error executing action: %v", err),
						"output": string(output),
					}
				} else {
					if err = json.Unmarshal(output, &resp); err != nil {
						resp = map[string]interface{}{
							"error":      "Error parsing action output",
							"raw_output": string(output),
						}
					}
				}
				break
			}
		}

		// If the action is not found locally, delegate to the Gemini API for a conversational response.
		if !actionFound {
			resp = handleConversational(req.Action)
		}
	}

	// Log both the request and response.
	logToDB(req.Action, req, resp)
	jsonResponse(w, resp)
}

// parsePrefix checks if the input starts with a known two–character prefix followed by a space.
func parsePrefix(input string) (string, string) {
	if len(input) >= 3 && input[2] == ' ' {
		prefix := input[:2]
		if prefix == "py" || prefix == "sh" {
			payload := strings.TrimSpace(input[3:])
			return prefix, payload
		}
	}
	return "", input
}

// handlePythonCommand uses the Gemini API to generate and execute Python code based on the payload.
func handlePythonCommand(payload string) map[string]interface{} {
	// Construct a custom prompt for Python code generation.
	promptText := "You are a Python code generation assistant. Generate a valid Python script that, when executed, prints a JSON object with a single key \"result\" containing the answer to the following query: " + payload

	// Construct the Gemini API request payload.
	geminiReq := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"parts": []map[string]string{
					{"text": promptText},
				},
			},
		},
	}
	reqBody, err := json.Marshal(geminiReq)
	if err != nil {
		return map[string]interface{}{"error": "Error marshalling Gemini request"}
	}

	url := fmt.Sprintf("%s?key=%s", config.GeminiEndpoint, config.GeminiAPIKey)
	httpResp, err := http.Post(url, "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return map[string]interface{}{"error": "Error calling Gemini API"}
	}
	defer httpResp.Body.Close()
	body, err := ioutil.ReadAll(httpResp.Body)
	if err != nil {
		return map[string]interface{}{"error": "Error reading Gemini response"}
	}

	// Log Gemini raw response.
	log.Printf("Gemini raw response: %s", string(body))
	var geminiResp GeminiResponse
	if err = json.Unmarshal(body, &geminiResp); err != nil {
		return map[string]interface{}{
			"error":        "Error parsing Gemini response",
			"raw_response": string(body),
		}
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return map[string]interface{}{
			"error":        "No content generated by Gemini",
			"raw_response": string(body),
		}
	}

	generatedCode := geminiResp.Candidates[0].Content.Parts[0].Text
	log.Printf("Generated Python code: %s", generatedCode)

	// Write the generated code to a temporary file.
	tmpFile, err := ioutil.TempFile("", "hartley_generated_*.py")
	if err != nil {
		log.Printf("Error creating temporary file for generated code: %v", err)
		return geminiRespToMap(geminiResp)
	}
	defer os.Remove(tmpFile.Name())
	_, err = tmpFile.Write([]byte(generatedCode))
	tmpFile.Close()
	if err != nil {
		log.Printf("Error writing generated code to temporary file: %v", err)
		return geminiRespToMap(geminiResp)
	}

	// Execute the temporary Python file using our runPython fallback.
	pythonOutput, err := runPython(tmpFile.Name())
	log.Printf("Python execution output: %s", string(pythonOutput))
	if err != nil {
		log.Printf("Error executing generated code: %v", err)
		return geminiRespToMap(geminiResp)
	}

	var parsedResp map[string]interface{}
	if err = json.Unmarshal(pythonOutput, &parsedResp); err != nil {
		log.Printf("Error parsing output from generated code: %v", err)
		// Attempt to extract a valid JSON object from the output.
		parsedResp, err = extractValidJSON(pythonOutput)
		if err != nil {
			log.Printf("Error extracting valid JSON: %v", err)
			return geminiRespToMap(geminiResp)
		}
	}

	return parsedResp
}

// handleShellCommand executes a shell command using the local Python action runner.
func handleShellCommand(payload string) map[string]interface{} {
	// Prepare parameters as JSON.
	params := map[string]string{
		"command": payload,
	}
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return map[string]interface{}{"error": "Error marshalling shell command parameters"}
	}

	// Execute the "run_terminal_command" function from the Python action runner.
	output, err := runPython("python/action_runner.py", "run_terminal_command", string(paramsJSON))
	if err != nil {
		return map[string]interface{}{
			"error":  fmt.Sprintf("Error executing shell command: %v", err),
			"output": string(output),
		}
	}

	var resp map[string]interface{}
	if err = json.Unmarshal(output, &resp); err != nil {
		// Attempt to extract valid JSON from output.
		resp, err = extractValidJSON(output)
		if err != nil {
			return map[string]interface{}{
				"error":      "Error parsing shell command output",
				"raw_output": string(output),
			}
		}
	}

	return resp
}

// handleConversational uses the Gemini API to generate a conversational Python code response.
func handleConversational(query string) map[string]interface{} {
	promptText := "You are a helpful AI assistant. Pretend you are an AI named 'Hartley'. 'Hartley' was developed by Micah Crandell. 'Hartley' is running on his device. Hartley is solely a Python code generator/assistant. Hartley's working environment is in Micah's python terminal. Hartley has complete access to Micah's device through the python environment. Your task is to generate valid Python code that, when executed, prints a valid JSON string to standard output. The JSON must be an object with a single key \"result\" whose value is the answer to the user's query. The generated code must include all necessary import statements so that it runs correctly. You must provide the complete python script. Your output must consist solely of plain Python code without any markdown formatting, triple backticks, or code fences. The code must start with a print() statement, and nothing else should be included. Now, process the following query by writing python code: " + query

	// Construct the Gemini API request payload.
	geminiReq := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"parts": []map[string]string{
					{"text": promptText},
				},
			},
		},
	}
	reqBody, err := json.Marshal(geminiReq)
	if err != nil {
		return map[string]interface{}{"error": "Error marshalling Gemini request"}
	}

	url := fmt.Sprintf("%s?key=%s", config.GeminiEndpoint, config.GeminiAPIKey)
	httpResp, err := http.Post(url, "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return map[string]interface{}{"error": "Error calling Gemini API"}
	}
	defer httpResp.Body.Close()
	body, err := ioutil.ReadAll(httpResp.Body)
	if err != nil {
		return map[string]interface{}{"error": "Error reading Gemini response"}
	}

	// Log Gemini raw response.
	log.Printf("Gemini raw response: %s", string(body))
	var geminiResp GeminiResponse
	if err = json.Unmarshal(body, &geminiResp); err != nil {
		return map[string]interface{}{
			"error":        "Error parsing Gemini response",
			"raw_response": string(body),
		}
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return map[string]interface{}{
			"error":        "No content generated by Gemini",
			"raw_response": string(body),
		}
	}

	generatedCode := geminiResp.Candidates[0].Content.Parts[0].Text
	log.Printf("Generated Python code: %s", generatedCode)

	// Write the generated code to a temporary file.
	tmpFile, err := ioutil.TempFile("", "hartley_generated_*.py")
	if err != nil {
		log.Printf("Error creating temporary file for generated code: %v", err)
		return geminiRespToMap(geminiResp)
	}
	defer os.Remove(tmpFile.Name())
	_, err = tmpFile.Write([]byte(generatedCode))
	tmpFile.Close()
	if err != nil {
		log.Printf("Error writing generated code to temporary file: %v", err)
		return geminiRespToMap(geminiResp)
	}

	// Execute the temporary Python file using our runPython fallback.
	pythonOutput, err := runPython(tmpFile.Name())
	log.Printf("Python execution output: %s", string(pythonOutput))
	if err != nil {
		log.Printf("Error executing generated code: %v", err)
		return geminiRespToMap(geminiResp)
	}

	var parsedResp map[string]interface{}
	if err = json.Unmarshal(pythonOutput, &parsedResp); err != nil {
		log.Printf("Error parsing output from generated code: %v", err)
		parsedResp, err = extractValidJSON(pythonOutput)
		if err != nil {
			log.Printf("Error extracting valid JSON: %v", err)
			return geminiRespToMap(geminiResp)
		}
	}

	return parsedResp
}

// geminiRespToMap converts a GeminiResponse to a map[string]interface{} for returning to the client.
func geminiRespToMap(geminiResp GeminiResponse) map[string]interface{} {
	// Marshal and unmarshal to convert to a generic map.
	data, err := json.Marshal(geminiResp)
	if err != nil {
		return map[string]interface{}{"error": "Error converting Gemini response"}
	}
	var result map[string]interface{}
	if err = json.Unmarshal(data, &result); err != nil {
		return map[string]interface{}{"error": "Error converting Gemini response"}
	}
	return result
}

// extractValidJSON attempts to extract a valid JSON object from the given output.
// It first tries to unmarshal the complete output, then scans line-by-line (from bottom up)
// for the first valid JSON object.
func extractValidJSON(output []byte) (map[string]interface{}, error) {
	var result map[string]interface{}
	if err := json.Unmarshal(output, &result); err == nil {
		return result, nil
	}
	lines := strings.Split(string(output), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if err := json.Unmarshal([]byte(line), &result); err == nil {
			return result, nil
		}
	}
	return nil, errors.New("no valid JSON found in output")
}

// jsonResponse sends a JSON response.
func jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// logToDB logs the action request and response into the database.
func logToDB(action string, request interface{}, response interface{}) {
	reqJSON, _ := json.Marshal(request)
	respJSON, _ := json.Marshal(response)
	_, err := dbConn.Exec(
		"INSERT INTO logs(timestamp, action, request, response) VALUES (?, ?, ?, ?)",
		time.Now(), action, string(reqJSON), string(respJSON),
	)
	if err != nil {
		log.Printf("Error logging to database: %v", err)
	}
}
