// tools/setup_onnx/main.go
//
// Downloads the DistilGPT-2 ONNX model and tokenizer files required by the
// ONNX prediction engine. Also installs the ONNX Runtime shared library if
// not already present.
//
// Usage:
//   go run ./tools/setup_onnx/              # downloads to ./model/
//   go run ./tools/setup_onnx/ --dir /opt/zen-typo/model
//
// After running:
//   1. Set "engine": "onnx" in config.json
//   2. Rebuild: go build -tags onnx .
//
// Files downloaded (~45MB total):
//   model/distilgpt2.onnx    — quantized DistilGPT-2 model (40MB)
//   model/vocab.json         — GPT-2 BPE vocabulary (1.0MB)
//   model/merges.txt         — BPE merge rules (456KB)

package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

const (
	// Xenova's pre-exported ONNX DistilGPT-2 (int8 quantized, 40MB)
	onnxModelURL = "https://huggingface.co/Xenova/distilgpt2/resolve/main/onnx/decoder_model_quantized.onnx"
	vocabURL     = "https://huggingface.co/gpt2/resolve/main/vocab.json"
	mergesURL    = "https://huggingface.co/gpt2/resolve/main/merges.txt"

	// ONNX Runtime 1.17.3 for Linux x86_64
	onnxRuntimeURL = "https://github.com/microsoft/onnxruntime/releases/download/v1.17.3/onnxruntime-linux-x64-1.17.3.tgz"
)

func main() {
	dir := flag.String("dir", "model", "Directory to save model files")
	skipRuntime := flag.Bool("skip-runtime", false, "Skip ONNX Runtime installation check")
	flag.Parse()

	if err := os.MkdirAll(*dir, 0755); err != nil {
		log.Fatalf("mkdir %s: %v", *dir, err)
	}

	files := []struct {
		url  string
		dest string
		desc string
	}{
		{onnxModelURL, filepath.Join(*dir, "distilgpt2.onnx"), "DistilGPT-2 ONNX model (~40MB)"},
		{vocabURL, filepath.Join(*dir, "vocab.json"), "GPT-2 vocabulary"},
		{mergesURL, filepath.Join(*dir, "merges.txt"), "GPT-2 BPE merges"},
	}

	for _, f := range files {
		if _, err := os.Stat(f.dest); err == nil {
			log.Printf("✓ %s already exists, skipping", f.dest)
			continue
		}
		log.Printf("Downloading %s → %s", f.desc, f.dest)
		if err := download(f.url, f.dest); err != nil {
			log.Fatalf("Download failed: %v", err)
		}
		log.Printf("✓ %s", f.dest)
	}

	if !*skipRuntime {
		checkOnnxRuntime()
	}

	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════════")
	fmt.Println("  Model files ready. Next steps:")
	fmt.Println()
	fmt.Printf("  1. Update config.json:\n")
	fmt.Printf("     \"engine\": \"onnx\",\n")
	fmt.Printf("     \"onnx_model_path\": \"%s\",\n", filepath.Join(*dir, "distilgpt2.onnx"))
	fmt.Printf("     \"onnx_vocab_path\": \"%s\"\n", filepath.Join(*dir, "vocab.json"))
	fmt.Println()
	fmt.Println("  2. Add Go dep (once):")
	fmt.Println("     go get github.com/yalue/onnxruntime_go")
	fmt.Println()
	fmt.Println("  3. Rebuild with ONNX tag:")
	fmt.Println("     go build -tags onnx .")
	fmt.Println("═══════════════════════════════════════════════════════")
}

func download(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}

	tmp := dest + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)

	written, err := io.Copy(f, resp.Body)
	f.Close()
	if err != nil {
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		return err
	}
	log.Printf("  → %.1f MB", float64(written)/1e6)
	return nil
}

func checkOnnxRuntime() {
	// Check if libonnxruntime.so is already on the system
	paths := []string{
		"/usr/local/lib/libonnxruntime.so",
		"/usr/lib/libonnxruntime.so",
		"/usr/lib/x86_64-linux-gnu/libonnxruntime.so",
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			log.Printf("✓ ONNX Runtime found at %s", p)
			return
		}
	}

	fmt.Println()
	fmt.Println("⚠  ONNX Runtime shared library not found. Install it:")
	fmt.Println()
	fmt.Printf("  wget %s\n", onnxRuntimeURL)
	fmt.Println("  tar -xf onnxruntime-linux-x64-1.17.3.tgz")
	fmt.Println("  sudo cp onnxruntime-linux-x64-1.17.3/lib/libonnxruntime.so* /usr/local/lib/")
	fmt.Println("  sudo ldconfig")
	fmt.Println()
}
