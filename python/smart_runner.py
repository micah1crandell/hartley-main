#!/usr/bin/env python3
"""
smart_runner.py

This script is responsible for reading a generated Python code file,
ensuring that any missing imports are installed (with retries), and
executing the code safely with robust error handling.
"""

import ast
import sys
import subprocess
import importlib.util
import re
import os
import time
import traceback
import json
import logging

# Set up logging to a file for debugging and audit purposes.
logging.basicConfig(
    filename='smart_runner.log',
    level=logging.DEBUG,
    format='%(asctime)s [%(levelname)s] %(message)s'
)

def sanitize_generated_code(code: str) -> str:
    """
    Remove markdown code fences and other artifacts from the generated code.
    
    Args:
        code: The raw generated code as a string.
    
    Returns:
        Sanitized code as a string.
    """
    try:
        # Remove any opening markdown code fence for Python.
        code = re.sub(r"^```python\s*\n", "", code, flags=re.MULTILINE)
        # Remove any closing markdown code fences.
        code = code.replace("```", "")
    except Exception as e:
        logging.error(f"Error sanitizing code: {e}")
    return code

def retry_command(command: list, retries: int = 3, delay: int = 2) -> None:
    """
    Execute a command using subprocess with retry logic.
    
    Args:
        command: The command to run as a list (e.g., [sys.executable, "-m", "pip", "install", module]).
        retries: Number of attempts.
        delay: Seconds to wait between attempts.
    
    Raises:
        Exception if the command fails in all attempts.
    """
    for attempt in range(1, retries + 1):
        try:
            subprocess.check_call(command, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            logging.info(f"Successfully executed command: {' '.join(command)}")
            return
        except subprocess.CalledProcessError as e:
            logging.warning(f"Attempt {attempt} failed for command: {' '.join(command)} with error: {e}")
            if attempt < retries:
                time.sleep(delay)
            else:
                raise

def install_missing_imports(code: str) -> str:
    """
    Analyzes the generated code for import statements and installs any missing modules.
    Uses retry logic for module installation.
    
    Args:
        code: The generated code as a string.
    
    Returns:
        The sanitized code (unchanged) if dependencies are met.
    
    Exits:
        The process with an error message if installation fails.
    """
    code = sanitize_generated_code(code)
    try:
        tree = ast.parse(code)
    except Exception as e:
        error_msg = f"Error parsing code: {e}"
        logging.error(error_msg)
        print(json.dumps({"result": error_msg}))
        sys.exit(1)
    
    modules = set()
    for node in ast.walk(tree):
        if isinstance(node, ast.Import):
            for alias in node.names:
                modules.add(alias.name.split('.')[0])
        elif isinstance(node, ast.ImportFrom):
            if node.module:
                modules.add(node.module.split('.')[0])
    
    for module in modules:
        # Skip built-in modules.
        if module in sys.builtin_module_names:
            continue
        # If the module is not already installed, attempt to install it.
        if importlib.util.find_spec(module) is None:
            try:
                logging.info(f"Module '{module}' not found. Attempting installation.")
                retry_command([sys.executable, "-m", "pip", "install", module])
            except Exception as e:
                error_msg = f"Error installing module '{module}': {e}"
                logging.error(error_msg)
                print(json.dumps({"result": error_msg}))
                sys.exit(1)
    return code

def execute_generated_code(code: str) -> None:
    """
    Executes the generated Python code in a controlled environment.
    
    Args:
        code: The sanitized Python code to execute.
    
    Exits:
        The process with a JSON error message if execution fails.
    """
    exec_globals = {}
    try:
        exec(code, exec_globals)
    except Exception as e:
        tb = traceback.format_exc()
        error_msg = f"Error executing generated code: {e}. Traceback: {tb}"
        logging.error(error_msg)
        print(json.dumps({"result": error_msg}))
        sys.exit(1)

def main():
    if len(sys.argv) != 2:
        error_msg = "Error: Expected exactly one argument for the generated code file."
        logging.error(error_msg)
        print(json.dumps({"result": error_msg}))
        sys.exit(1)
    
    generated_file = sys.argv[1]
    
    try:
        with open(generated_file, "r") as f:
            generated_code = f.read()
    except Exception as e:
        error_msg = f"Error reading generated code file: {e}"
        logging.error(error_msg)
        print(json.dumps({"result": error_msg}))
        sys.exit(1)
    
    # Install any missing dependencies needed for the generated code.
    sanitized_code = install_missing_imports(generated_code)
    
    # Suppress stderr temporarily to prevent unwanted warnings from mixing with output.
    original_stderr = sys.stderr
    sys.stderr = open(os.devnull, 'w')
    
    try:
        execute_generated_code(sanitized_code)
    finally:
        sys.stderr.close()
        sys.stderr = original_stderr

if __name__ == "__main__":
    try:
        main()
    except Exception as unexpected_error:
        error_msg = f"Unexpected error: {unexpected_error}. Traceback: {traceback.format_exc()}"
        logging.critical(error_msg)
        print(json.dumps({"result": error_msg}))
        sys.exit(1)
