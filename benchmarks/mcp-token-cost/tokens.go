package main

import (
	"fmt"
	"sync"

	"github.com/pkoukk/tiktoken-go"
	tiktokenloader "github.com/pkoukk/tiktoken-go-loader"
)

// encodingName is the tokenizer this benchmark reports in. It is declared in
// results.json and cannot change without regenerating it: two encodings do not
// produce comparable counts.
const encodingName = "cl100k_base"

// counter counts tokens the way a model is billed for them.
//
// Bytes are not a usable proxy here. This phase exists to remove opaque base32
// identifiers and fully qualified type paths from responses, and those cost
// far more per byte than prose: a 52-character stable key is 35 tokens, 1.5
// characters each, while a line of Go source averages above 3. Measuring bytes
// would hide most of what the phase is trying to buy.
type counter struct {
	encoding *tiktoken.Tiktoken
}

var loadOffline sync.Once

// newCounter loads the embedded BPE ranks. The offline loader is what keeps the
// benchmark hermetic: the default loader downloads the vocabulary on first use,
// which would make a gate depend on network reachability.
func newCounter() (*counter, error) {
	loadOffline.Do(func() {
		tiktoken.SetBpeLoader(tiktokenloader.NewOfflineLoader())
	})
	encoding, err := tiktoken.GetEncoding(encodingName)
	if err != nil {
		return nil, fmt.Errorf("load %s encoding: %w", encodingName, err)
	}
	return &counter{encoding: encoding}, nil
}

func (c *counter) count(text string) int {
	return len(c.encoding.Encode(text, nil, nil))
}
