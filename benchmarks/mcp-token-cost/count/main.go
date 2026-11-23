// Command count prints how many tokens each of a set of texts costs, counted
// the way `benchmarks/mcp-token-cost` counts them.
//
// The harness bills the host's own captured output, and a capture is stored as
// its token count. This is the counter for a recapture: it reads a JSON object
// of `{"key": "text"}` on stdin and writes `{"key": tokens}` on stdout, with the
// same offline `cl100k_base` encoding as the harness, so a number taken here is
// one the harness would have computed.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/pkoukk/tiktoken-go"
	tiktokenloader "github.com/pkoukk/tiktoken-go-loader"
)

// encodingName must be the harness's: two encodings do not produce comparable
// counts.
const encodingName = "cl100k_base"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "count:", err)
		os.Exit(1)
	}
}

func run() error {
	tiktoken.SetBpeLoader(tiktokenloader.NewOfflineLoader())
	encoding, err := tiktoken.GetEncoding(encodingName)
	if err != nil {
		return fmt.Errorf("load %s encoding: %w", encodingName, err)
	}
	texts := map[string]string{}
	if err := json.NewDecoder(os.Stdin).Decode(&texts); err != nil {
		return fmt.Errorf("read texts from stdin: %w", err)
	}
	counts := make(map[string]int, len(texts))
	for key, text := range texts {
		counts[key] = len(encoding.Encode(text, nil, nil))
	}
	return json.NewEncoder(os.Stdout).Encode(counts)
}
