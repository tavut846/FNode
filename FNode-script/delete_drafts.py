#!/usr/bin/env python3
"""
Utility script to scan and delete all draft releases on GitHub.

Usage:
    export GITHUB_TOKEN="ghp_xxx"
    python FNode-script/delete_drafts.py

Or:
    python FNode-script/delete_drafts.py --token "ghp_xxx" --repo "tavut846/FNode"
"""

import sys
import os
import argparse
import json
import urllib.request
import urllib.error

def main():
    parser = argparse.ArgumentParser(description="Delete all draft releases from a GitHub repository.")
    parser.add_argument("--repo", default="tavut846/FNode", help="GitHub repository (owner/repo). Default: tavut846/FNode")
    parser.add_argument("--token", default=os.getenv("GITHUB_TOKEN") or os.getenv("GH_TOKEN"), help="GitHub Personal Access Token (defaults to GITHUB_TOKEN or GH_TOKEN env var)")
    args = parser.parse_args()

    token = args.token
    if not token:
        print("Error: GitHub token is required. Please set GITHUB_TOKEN or GH_TOKEN, or pass --token.", file=sys.stderr)
        sys.exit(1)

    repo = args.repo
    headers = {
        "Authorization": f"Bearer {token}",
        "Accept": "application/vnd.github+json",
        "User-Agent": "FNode-Draft-Cleaner",
        "X-GitHub-Api-Version": "2022-11-28",
    }

    print(f"Scanning for draft releases in {repo}...")
    page = 1
    drafts = []

    while True:
        url = f"https://api.github.com/repos/{repo}/releases?per_page=100&page={page}"
        req = urllib.request.Request(url, headers=headers)
        try:
            with urllib.request.urlopen(req) as resp:
                data = json.loads(resp.read().decode())
                if not data:
                    break
                for r in data:
                    if r.get("draft"):
                        drafts.append(r)
                if len(data) < 100:
                    break
                page += 1
        except urllib.error.HTTPError as e:
            print(f"Error fetching releases: HTTP {e.code} - {e.reason}", file=sys.stderr)
            sys.exit(1)

    if not drafts:
        print(f"No draft releases found in {repo}.")
        return

    print(f"Found {len(drafts)} draft release(s). Deleting...")
    deleted = 0
    for d in drafts:
        d_id = d.get("id")
        d_tag = d.get("tag_name") or "no-tag"
        d_name = d.get("name") or "unnamed"
        del_url = f"https://api.github.com/repos/{repo}/releases/{d_id}"
        del_req = urllib.request.Request(del_url, headers=headers, method="DELETE")
        try:
            with urllib.request.urlopen(del_req) as resp:
                if resp.status in (204, 200):
                    print(f"  [DELETED] ID: {d_id} | Tag: {d_tag} | Name: {d_name}")
                    deleted += 1
        except urllib.error.HTTPError as e:
            print(f"  [FAILED] ID: {d_id} - HTTP {e.code}: {e.reason}", file=sys.stderr)

    print(f"\nCleanup complete. Successfully deleted {deleted} of {len(drafts)} draft release(s).")

if __name__ == "__main__":
    main()
