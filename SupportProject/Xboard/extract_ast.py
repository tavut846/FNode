import sys
import json
from graphify.extract import collect_files, extract
from pathlib import Path

def main():
    code_files = []
    try:
        with open('graphify-out/.graphify_detect.json', encoding='utf-8') as f:
            detect = json.load(f)
    except Exception as e:
        print(f"Error loading detect.json: {e}")
        sys.exit(1)

    for f in detect.get('files', {}).get('code', []):
        p = Path(f)
        if p.is_dir():
            code_files.extend(collect_files(p))
        else:
            code_files.append(p)

    if code_files:
        print(f"Extracting AST from {len(code_files)} files...")
        result = extract(code_files, cache_root=Path('.'))
        with open('graphify-out/.graphify_ast.json', 'w', encoding='utf-8') as f:
            json.dump(result, f, indent=2)
        print(f'AST: {len(result["nodes"])} nodes, {len(result["edges"])} edges')
    else:
        with open('graphify-out/.graphify_ast.json', 'w', encoding='utf-8') as f:
            json.dump({'nodes':[], 'edges':[], 'input_tokens':0, 'output_tokens':0}, f)
        print('No code files - skipping AST extraction')

if __name__ == '__main__':
    main()
