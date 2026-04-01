from pathlib import Path
import re
root = Path(r'C:/Users/Administrator/Desktop/Rana-Remote')
patterns = [r'\bUpsertSettings\b', r'store\.Settings\b', r'\bSettings\b']
for p in root.rglob('*.go'):
    text = p.read_text(encoding='utf-8', errors='ignore').splitlines()
    for i, line in enumerate(text, 1):
        if any(re.search(pat, line) for pat in patterns):
            print(f'{p}:{i}:{line.strip()}')
