#!/usr/bin/env python3
import ast
import sys
import subprocess
import importlib.util
import re
import os

def sanitize_generated_code(code):
    # Remove Markdown code fences.
    code = re.sub(r"^```python\s*\n", "", code, flags=re.MULTILINE)
    code = code.replace("```", "")
    return code

def install_missing_imports(code):
    # Sanitize the code first.
    code = sanitize_generated_code(code)
    try:
        tree = ast.parse(code)
    except Exception as e:
        print('{"result": "Error parsing code: ' + str(e).replace('"', "'") + '"}')
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
        if module in sys.builtin_module_names:
            continue
        if importlib.util.find_spec(module) is None:
            try:
                subprocess.check_call(
                    [sys.executable, "-m", "pip", "install", module],
                    stdout=subprocess.DEVNULL,
                    stderr=subprocess.DEVNULL
                )
            except subprocess.CalledProcessError as e:
                print('{"result": "Error installing module ' + module + ': ' + str(e).replace('"', "'") + '"}')
                sys.exit(1)
    return code

def main():
    if len(sys.argv) != 2:
        print('{"result": "Error: Expected exactly one argument for the generated code file."}')
        sys.exit(1)
    generated_file = sys.argv[1]
    try:
        with open(generated_file, "r") as f:
            generated_code = f.read()
    except Exception as e:
        print('{"result": "Error reading generated code file."}')
        sys.exit(1)
    sanitized_code = install_missing_imports(generated_code)
    
    # Temporarily suppress stderr to avoid warnings mixing into output.
    original_stderr = sys.stderr
    sys.stderr = open(os.devnull, 'w')
    
    try:
        exec_globals = {}
        exec(sanitized_code, exec_globals)
    except Exception as e:
        error_msg = str(e).replace('"', "'")
        print('{"result": "Error executing generated code: ' + error_msg + '"}')
        sys.exit(1)
    finally:
        sys.stderr = original_stderr

if __name__ == "__main__":
    main()
