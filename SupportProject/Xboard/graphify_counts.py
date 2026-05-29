import json
from pathlib import Path

with open('graphify-out/.graphify_detect.json') as f:
    d = json.load(f)

root = Path('.').absolute()
counts = {}
all_files = [f for files in d['files'].values() for f in files]
for f in all_files:
    p = Path(f).absolute()
    try:
        rel = p.relative_to(root)
        if len(rel.parts) > 0:
            sub = rel.parts[0]
            counts[sub] = counts.get(sub, 0) + 1
    except ValueError:
        continue

sorted_counts = sorted(counts.items(), key=lambda x: x[1], reverse=True)
print(json.dumps(sorted_counts[:5]))
