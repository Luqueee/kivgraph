//go:build !(darwin || linux)

// Command source-verification needs the inode change time of a file, which only
// the unix stat exposes.
package main

import "fmt"

func main() { fmt.Println("source-verification is only available on unix platforms") }
