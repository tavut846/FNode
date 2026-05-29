import json
from pathlib import Path
import math

with open('graphify-out/.graphify_uncached.txt', encoding='utf-8') as f:
    files = [line.strip() for line in f if line.strip()]

chunk_size = 22
num_chunks = math.ceil(len(files) / chunk_size)

for i in range(num_chunks):
    chunk = files[i * chunk_size : (i + 1) * chunk_size]
    with open(f'graphify-out/.graphify_chunk_list_{i}.txt', 'w', encoding='utf-8') as f:
        f.write('\n'.join(chunk))

print(f"Split {len(files)} files into {num_chunks} chunks.")
