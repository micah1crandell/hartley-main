#!/usr/bin/env python3
import ast
import sys
import subprocess
import importlib.util

def install_missing_imports(code):
    # Parse the code to extract top-level module names.
    tree = ast.parse(code)
    modules = set()
    for node in ast.walk(tree):
        if isinstance(node, ast.Import):
            for alias in node.names:
                modules.add(alias.name.split('.')[0])
        elif isinstance(node, ast.ImportFrom):
            if node.module:
                modules.add(node.module.split('.')[0])
    # For each module, check if it is installed; if not, install it.
    for module in modules:
        if module in sys.builtin_module_names:
            continue
        if importlib.util.find_spec(module) is None:
            # Optional: you could print a message (or log it) here.
            subprocess.check_call([sys.executable, "-m", "pip", "install", module])

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
    # Install any missing modules
    install_missing_imports(generated_code)
    # Execute the generated code
    try:
        exec_globals = {}
        exec(generated_code, exec_globals)
    except Exception as e:
        # Ensure the error is output as a JSON string
        error_msg = str(e).replace('"', "'")
        print('{"result": "Error executing generated code: ' + error_msg + '"}')
        sys.exit(1)

if __name__ == "__main__":
    main()
