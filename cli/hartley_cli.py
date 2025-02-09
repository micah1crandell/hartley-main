#!/usr/bin/env python3
import sys
import json
import argparse
import requests

def main():
    parser = argparse.ArgumentParser(description="Hartley CLI")
    parser.add_argument("--server", default="http://localhost:8080", help="Hartley server address")
    parser.add_argument("--action", required=True, help="Action to perform")
    parser.add_argument("--params", default="{}", help="JSON string of parameters for the action")
    args = parser.parse_args()

    try:
        params = json.loads(args.params)
    except Exception as e:
        print(f"Error parsing parameters: {e}")
        sys.exit(1)

    payload = {"action": args.action, "params": params}
    url = f"{args.server}/api/action"
    try:
        response = requests.post(url, json=payload)
        print(json.dumps(response.json(), indent=2))
    except Exception as e:
        print(f"Error contacting server: {e}")

if __name__ == "__main__":
    main()
