import json

transcript_path = "/Users/fabian/.gemini/antigravity-cli/brain/c90e71f7-0c86-40d6-9519-4f72bf122caf/.system_generated/logs/transcript_full.jsonl"

print("Searching transcript_full.jsonl...")

with open(transcript_path, "r", encoding="utf-8") as f:
    for line_idx, line in enumerate(f):
        if "routes/scenarios/+page.svelte" in line:
            try:
                obj = json.loads(line)
                print(f"Line {line_idx + 1}: type={obj.get('type')}, source={obj.get('source')}, step_index={obj.get('step_index')}")
            except Exception as e:
                print(f"Line {line_idx + 1}: failed to parse JSON")
