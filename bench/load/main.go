package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	base     = flag.String("base", "http://127.0.0.1:18098", "server base URL")
	token    = flag.String("token", "", "JWT for authenticated endpoints")
	mode     = flag.String("mode", "list", "list|search|detail|timeline|create|login|claim")
	n        = flag.Int("n", 500, "requests per endpoint (latency mode)")
	conc     = flag.Int("c", 1, "concurrency (throughput mode)")
	dur      = flag.Duration("d", 0, "if >0, run concurrent load for this long instead of -n")
	email    = flag.String("email", "bench1@example.com", "login email")
	password = flag.String("password", "bench-password-1234", "login password")
	itemID   = flag.String("item", "", "item id for detail/timeline modes")
	itemFile = flag.String("itemfile", "", "file with one item id per line for claim mode")
	version  = flag.Int("version", 15, "item version for claim mode")
)

type result struct {
	d   time.Duration
	err bool
}

func do(client *http.Client, req *http.Request) result {
	start := time.Now()
	resp, err := client.Do(req)
	took := time.Since(start)
	if err != nil {
		return result{took, true}
	}
	resp.Body.Close()
	return result{took, resp.StatusCode >= 500}
}

func auth(req *http.Request) *http.Request {
	if *token != "" {
		req.Header.Set("Authorization", "Bearer "+*token)
	}
	return req
}

func get(url string) *http.Request {
	req, _ := http.NewRequest("GET", url, nil)
	return auth(req)
}

func post(url, body string) *http.Request {
	req, _ := http.NewRequest("POST", url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return auth(req)
}

func pct(vals []time.Duration, p float64) time.Duration {
	idx := int(float64(len(vals)) * p)
	if idx >= len(vals) {
		idx = len(vals) - 1
	}
	return vals[idx]
}

func latency(client *http.Client, name string, mk func(i int) *http.Request) {
	for i := 0; i < 50; i++ { // warmup
		do(client, mk(i))
	}
	res := make([]result, 0, *n)
	for i := 0; i < *n; i++ {
		res = append(res, do(client, mk(i)))
	}
	vals := make([]time.Duration, 0, len(res))
	var errs int
	for _, r := range res {
		vals = append(vals, r.d)
		if r.err {
			errs++
		}
	}
	sort.Slice(vals, func(a, b int) bool { return vals[a] < vals[b] })
	var sum time.Duration
	for _, v := range vals {
		sum += v
	}
	fmt.Printf("%-8s n=%d errs=%d p50=%v p95=%v p99=%v max=%v\n",
		name, *n, errs, pct(vals, 0.50).Round(time.Microsecond),
		pct(vals, 0.95).Round(time.Microsecond), pct(vals, 0.99).Round(time.Microsecond),
		vals[len(vals)-1].Round(time.Microsecond))
}

func throughput(client *http.Client, name string, mk func(i int) *http.Request) {
	var count, errs atomic.Int64
	deadline := time.Now().Add(*dur)
	var wg sync.WaitGroup
	for w := 0; w < *conc; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; ; i++ {
				if time.Now().After(deadline) {
					return
				}
				r := do(client, mk(worker*1000000+i))
				count.Add(1)
				if r.err {
					errs.Add(1)
				}
			}
		}(w)
	}
	wg.Wait()
	secs := dur.Seconds()
	fmt.Printf("%-8s c=%d %.0f req/s errs=%d over %.0fs\n",
		name, *conc, float64(count.Load())/secs, errs.Load(), secs)
}

func main() {
	flag.Parse()
	client := &http.Client{
		Transport: &http.Transport{MaxIdleConnsPerHost: 64},
		Timeout:   30 * time.Second,
	}
	var make func(i int) *http.Request
	switch *mode {
	case "list":
		make = func(i int) *http.Request { return get(*base + "/items?limit=25") }
	case "search":
		make = func(i int) *http.Request { return get(*base + "/items?limit=25&q=latency") }
	case "detail":
		make = func(i int) *http.Request { return get(*base + "/items/" + *itemID) }
	case "timeline":
		make = func(i int) *http.Request { return get(*base + "/items/" + *itemID + "/timeline") }
	case "login":
		make = func(i int) *http.Request {
			return post(*base+"/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, *email, *password))
		}
	case "create":
		make = func(i int) *http.Request {
			return post(*base+"/items", fmt.Sprintf(`{"title":"Bench item %d %d"}`, time.Now().UnixNano(), i))
		}
	case "claim":
		raw, err := os.ReadFile(*itemFile)
		if err != nil {
			panic(err)
		}
		ids := strings.Fields(string(raw))
		var idx atomic.Int64
		if len(ids) == 0 {
			panic("empty item file")
		}
		make = func(i int) *http.Request {
			id := ids[idx.Add(1)-1]
			return post(*base+"/items/"+id+"/activity/claim", fmt.Sprintf(`{"version":%d}`, *version))
		}
	default:
		panic("unknown mode " + *mode)
	}
	if *dur > 0 {
		throughput(client, *mode, make)
	} else {
		latency(client, *mode, make)
	}
}
