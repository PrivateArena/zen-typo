#!/bin/bash
# zen_typo_onnx.sh
# Automates building and launching zen-typo with the ONNX LLM engine.

set -e

# Change directory to the workspace path of zen-typo
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "=========================================================="
echo "  Preparing zen-typo ONNX (LLM) next-word predictor"
echo "=========================================================="

# 1. Download model files and shared library if missing
if [ ! -f "model/distilgpt2.onnx" ] || [ ! -f "model/vocab.json" ] || [ ! -f "libonnxruntime.so" ]; then
    echo "[ONNX] Model files or libonnxruntime.so missing. Running setup_onnx..."
    go run ./tools/setup_onnx/
else
    echo "[ONNX] Model files and libonnxruntime.so are already present."
fi

# 2. Rebuild the binary with ONNX support tags
echo "[ONNX] Rebuilding zen-typo binary with ONNX tags..."
go build -tags onnx .

# # 3. Configure config.json to use ONNX engine
# if [ -f "config.json" ]; then
#     echo "[ONNX] Updating config.json to set engine: onnx..."
#     python3 -c "
# import json
# with open('config.json', 'r') as f:
#     d = json.load(f)
# d['engine'] = 'onnx'
# with open('config.json', 'w') as f:
#     json.dump(d, f, indent=2)
# "
# else
#     echo "[ONNX] Creating a clean config.json with engine: onnx..."
#     cat <<EOF > config.json
# {
#   "engine": "onnx",
#   "ngram_db_path": "ngrams.db",
#   "onnx_model_path": "model/distilgpt2.onnx",
#   "onnx_vocab_path": "model/vocab.json",
#   "onnx_merges_path": "model/merges.txt",
#   "max_suggestions": 5
# }
# EOF
# fi

# 4. Start the daemon with local library path loaded
echo "[ONNX] Launching zen-typo in LLM prediction mode..."
export LD_LIBRARY_PATH=".:$LD_LIBRARY_PATH"
./zen-typo
