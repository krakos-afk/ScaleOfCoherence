// genhash prints a bcrypt hash for the benchmark password and, with -bench,
// measures bcrypt verification cost on this machine (see Scalability.md).
package main

import (
	"flag"
	"fmt"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func main() {
	bench := flag.Int("bench", 0, "run N hash comparisons and report timing")
	flag.Parse()
	if flag.NArg() < 1 {
		fmt.Println("usage: genhash [-bench N] <password>")
		flag.PrintDefaults()
		return
	}
	password := flag.Arg(0)

	start := time.Now()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s\n# generate cost=%d took=%v\n", hash, bcrypt.DefaultCost, time.Since(start))
	if *bench == 0 {
		return
	}

	n := *bench
	start = time.Now()
	for i := 0; i < n; i++ {
		_ = bcrypt.CompareHashAndPassword(hash, []byte(password))
	}
	per := time.Since(start) / time.Duration(n)
	fmt.Printf("# compare single-thread: %v per hash (%.1f hashes/s)\n", per, 1/per.Seconds())

	workers := 8
	var wg sync.WaitGroup
	start = time.Now()
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < n; i++ {
				_ = bcrypt.CompareHashAndPassword(hash, []byte(password))
			}
		}()
	}
	wg.Wait()
	total := workers * n
	fmt.Printf("# compare %d workers: %.1f hashes/s aggregate\n", workers, float64(total)/time.Since(start).Seconds())
}
