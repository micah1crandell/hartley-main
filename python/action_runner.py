#!/usr/bin/env python3
import sys
import json
import subprocess

def run_terminal_command(params):
    # Expects a "command" field in params.
    command = params.get("command")
    if not command:
        return {"error": "No command provided"}
    try:
        result = subprocess.run(command, shell=True, capture_output=True, text=True)
        return {
            "stdout": result.stdout,
            "stderr": result.stderr,
            "returncode": result.returncode
        }
    except Exception as e:
        return {"error": str(e)}

def create_website(params):
    # Create a simple HTML file.
    title = params.get("title", "My Website")
    body  = params.get("body", "<p>Hello, World!</p>")
    html_content = f"""<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>{title}</title>
</head>
<body>
  {body}
</body>
</html>"""
    try:
        with open("website.html", "w") as f:
            f.write(html_content)
        return {"message": "Website created successfully", "file": "website.html"}
    except Exception as e:
        return {"error": str(e)}

def turn_on_lights(params):
    # In a production system, this would send a command to your IoT system.
    # Here we simulate the action.
    return {"message": "Living room lights turned on"}

def main():
    if len(sys.argv) < 3:
        print(json.dumps({"error": "Insufficient arguments"}))
        sys.exit(1)
    function = sys.argv[1]
    try:
        params = json.loads(sys.argv[2])
    except Exception as e:
        print(json.dumps({"error": "Invalid JSON for parameters"}))
        sys.exit(1)

    if function == "run_terminal_command":
        result = run_terminal_command(params)
    elif function == "create_website":
        result = create_website(params)
    elif function == "turn_on_lights":
        result = turn_on_lights(params)
    else:
        result = {"error": "Unknown function"}
    print(json.dumps(result))

if __name__ == "__main__":
    main()
