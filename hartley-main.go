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

type Config struct {
	ServerPort     int    `json:"server_port"`
	GeminiAPIKey   string `json:"gemini_api_key"`
	GeminiEndpoint string `json:"gemini_endpoint"`
}

type Action struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Script      string `json:"script"`
	Function    string `json:"function"`
}

type ActionRequest struct {
	Action string                 `json:"action"`
	Params map[string]interface{} `json:"params"`
}

// ----------------------
// Updated Gemini Response Structures
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
	actionFound := false

	// Check if the requested action exists locally.
	for _, act := range actions {
		if act.Name == req.Action {
			actionFound = true
			paramsJSON, err := json.Marshal(req.Params)
			if err != nil {
				resp = map[string]interface{}{"error": "Error marshalling parameters"}
				break
			}
			// Execute the defined Python script using fallback for python3/python/py.
			output, err := runPython(act.Script, act.Function, string(paramsJSON))
			if err != nil {
				resp = map[string]interface{}{
					"error":  fmt.Sprintf("Error executing action: %v", err),
					"output": string(output),
				}
			} else {
				// Attempt to parse the output as JSON by locating the valid JSON substring.
				var parsedResp map[string]interface{}
				outputStr := string(output)
				jsonStartIndex := strings.Index(outputStr, `{"result"`)
				if jsonStartIndex == -1 {
					log.Printf("Error: valid JSON not found in output: %s", outputStr)
					resp = map[string]interface{}{
						"error": "Valid JSON not found in output",
					}
				} else {
					jsonStr := outputStr[jsonStartIndex:]
					if err = json.Unmarshal([]byte(jsonStr), &parsedResp); err != nil {
						log.Printf("Error parsing extracted JSON: %v", err)
						resp = map[string]interface{}{
							"error": "Error parsing extracted JSON",
						}
					} else {
						resp = parsedResp
					}
				}
			}
			break
		}
	}

	// If the action is not found locally, delegate to the Gemini API using our strict prompt.
	if !actionFound {
		// Use the user's query as the basis for our prompt.
		userQuery := req.Action
		// Construct the strict prompt.
		promptText := "You are a helpful AI assistant. Pretend you are an AI named 'Hartley'. 'Hartley' was developed by Micah Crandell. 'Hartley' is running on his device. Hartley is solely a Python code generator/assistant. Hartley's working environment is in Micah's python terminal. Hartley has complete access to Micah's device through the python environment. Your task is to generate valid Python code that, when executed, prints a valid JSON string to standard output. The JSON must be an object with a single key \"result\" whose value is the answer to the user's query. The generated code must include all necessary import statements so that it runs correctly. You must provide the complete python script. Your output must consist solely of plain Python code without any markdown formatting, triple backticks, or code fences. The code must start with a print() statement, and nothing else should be included. For example, if the query is: How does AI work? Your output should be exactly: print('{\"result\": \"AI works by processing data and learning patterns.\"}') Now, process the following query by writing python code: " + userQuery

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
			resp = map[string]interface{}{"error": "Error marshalling Gemini request"}
		} else {
			url := fmt.Sprintf("%s?key=%s", config.GeminiEndpoint, config.GeminiAPIKey)
			httpResp, err := http.Post(url, "application/json", bytes.NewBuffer(reqBody))
			if err != nil {
				resp = map[string]interface{}{"error": "Error calling Gemini API"}
			} else {
				defer httpResp.Body.Close()
				body, err := ioutil.ReadAll(httpResp.Body)
				if err != nil {
					resp = map[string]interface{}{"error": "Error reading Gemini response"}
				} else {
					// Log Gemini raw response.
					log.Printf("Gemini raw response: %s", string(body))
					var geminiResp GeminiResponse
					if err = json.Unmarshal(body, &geminiResp); err != nil {
						resp = map[string]interface{}{
							"error":        "Error parsing Gemini response",
							"raw_response": string(body),
						}
					} else {
						// Extract the generated code from the first candidate.
						if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
							resp = map[string]interface{}{
								"error":        "No content generated by Gemini",
								"raw_response": string(body),
							}
						} else {
							generatedCode := geminiResp.Candidates[0].Content.Parts[0].Text
							// Log the generated Python code.
							log.Printf("Generated Python code: %s", generatedCode)
							// Write the generated code to a temporary file.
							tmpFile, err := ioutil.TempFile("", "hartley_generated_*.py")
							if err != nil {
								log.Printf("Error creating temporary file for generated code: %v", err)
								// Return the initial Gemini response.
								resp = geminiRespToMap(geminiResp)
							} else {
								defer os.Remove(tmpFile.Name())
								_, err = tmpFile.Write([]byte(generatedCode))
								tmpFile.Close()
								if err != nil {
									log.Printf("Error writing generated code to temporary file: %v", err)
									resp = geminiRespToMap(geminiResp)
								} else {
									// Execute the temporary Python file using our runPython fallback.
									pythonOutput, err := runPython(tmpFile.Name())
									// Log the console output from the Python execution.
									log.Printf("Python execution output: %s", string(pythonOutput))
									if err != nil {
										log.Printf("Error executing generated code: %v", err)
										// Return the initial Gemini response.
										resp = geminiRespToMap(geminiResp)
									} else {
										// Attempt to parse the output as JSON by locating the valid JSON substring.
										var parsedResp map[string]interface{}
										outputStr := string(pythonOutput)
										jsonStartIndex := strings.Index(outputStr, `{"result"`)
										if jsonStartIndex == -1 {
											log.Printf("Error: valid JSON not found in output: %s", outputStr)
											resp = geminiRespToMap(geminiResp)
										} else {
											jsonStr := outputStr[jsonStartIndex:]
											if err = json.Unmarshal([]byte(jsonStr), &parsedResp); err != nil {
												log.Printf("Error parsing extracted JSON: %v", err)
												resp = geminiRespToMap(geminiResp)
											} else {
												resp = parsedResp
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}

	// Log both the request and response.
	logToDB(req.Action, req, resp)
	jsonResponse(w, resp)
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

// ----------------------
// Utility Functions
// ----------------------

func jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

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
