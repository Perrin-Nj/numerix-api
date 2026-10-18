// Command loadtest simulates many kids playing against a running Numerix API and prints latency
// percentiles and status counts per endpoint. It uses only the standard library.
//
// Go for Spring people: this is a separate program (package main) next to cmd/api. Think of it as a
// small JMeter/Gatling script. "Open loop" means requests start on a clock (target requests per second)
// whether or not earlier ones finished, so a slow server shows up as latency, not as fewer requests.
//
// Server settings needed (kids come from many fake IPs so the per-IP limits do not mask capacity):
//
//	PORT=8090 DB_PATH=./data/load.db APP_ENV=development FEATURE_WELCOME_GIFT=false TRUST_PROXY=true
package main

import (
	"bytes"              // build request bodies in memory
	"context"            // dial hook signature
	"crypto/rand"        // random UUIDs for Idempotency-Key
	"encoding/json"      // JSON encode/decode (Spring: Jackson)
	"flag"               // command line flags
	"fmt"                // formatted printing
	"io"                 // drain response bodies
	mrand "math/rand/v2" // random choice of answer
	"net"                // dialer for the TCP connect timing
	"net/http"           // HTTP client
	"os"                 // exit codes
	"sort"               // sort latencies for percentiles
	"strconv"            // string/number conversion
	"strings"            // parse the -stages flag
	"sync"               // mutexes and WaitGroup (Spring: synchronized / CountDownLatch)
	"sync/atomic"        // lock-free flags and counters
	"time"               // clocks and durations
)

// sample is one finished request.
type sample struct {
	status int           // HTTP status, or 0 when the connection failed
	dur    time.Duration // time from send to full body read
}

// recorder keeps all samples per endpoint name (Spring: a Micrometer registry, but dumb and exact).
type recorder struct {
	mu sync.Mutex          // protects the map below, many goroutines write at once
	m  map[string][]sample // endpoint name -> samples
}

// add stores one sample under an endpoint name.
func (r *recorder) add(name string, status int, d time.Duration) {
	r.mu.Lock()         // take the lock
	defer r.mu.Unlock() // release when add returns
	r.m[name] = append(r.m[name], sample{status, d})
}

// kid is one virtual child and the game state it holds between calls.
type kid struct {
	busy      atomic.Bool       // true while a request of this kid is in flight (one at a time, like a real child)
	ip        string            // fake client IP sent in X-Forwarded-For
	token     string            // child access token
	childID   string            // child profile id
	sessionID string            // current session, "" when none
	beatID    string            // open (unanswered) beat, "" when none
	choices   []json.RawMessage // choices of the open beat, kept raw so any JSON type is sent back unchanged
}

// ipRun is the second octet of every fake IP, random per run so repeated runs on one server do not hit the 5/hour register limit.
var ipRun = 1 + mrand.IntN(250)

// client wraps the HTTP client and the target.
type client struct {
	base string       // e.g. http://localhost:8090
	http *http.Client // shared connection pool
	rec  *recorder    // where samples go
	xff  bool         // send X-Forwarded-For per kid
}

// uuid makes a random version 4 UUID string for Idempotency-Key.
func uuid() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40 // version 4
	b[8] = b[8]&0x3f | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// do sends one request, records it under name and returns status and raw body.
// record=false is used for setup calls that must not pollute the numbers.
func (c *client) do(name, method, path, token, ip string, body any, record bool) (int, []byte) {
	var rd io.Reader // request body, nil when there is none
	if body != nil {
		b, _ := json.Marshal(body) // our own maps always marshal
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, rd)
	if err != nil {
		return 0, nil
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", uuid())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if c.xff && ip != "" {
		req.Header.Set("X-Forwarded-For", ip)
	}
	start := time.Now()
	resp, err := c.http.Do(req)
	if err != nil { // connection refused, timeout...
		if record {
			c.rec.add(name, 0, time.Since(start))
		}
		return 0, nil
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body) // read fully so the latency includes the body
	if record {
		c.rec.add(name, resp.StatusCode, time.Since(start))
	}
	return resp.StatusCode, data
}

// setupFamily registers a parent, consents, creates a child and gets a child token with the PIN.
func (c *client) setupFamily(i int, nonce string) (*kid, error) {
	k := &kid{ip: fmt.Sprintf("10.%d.%d.%d", ipRun, (i>>8)&255, i&255)} // unique fake IP per family
	email := fmt.Sprintf("load-%s-%d@example.com", nonce, i)
	code, b := c.do("setup", "POST", "/v1/auth/register", "", k.ip,
		map[string]any{"email": email, "password": "load-test-password-1", "locale": "fr-CA"}, false)
	if code != 201 {
		return nil, fmt.Errorf("register %d: status %d %s", i, code, b)
	}
	var auth struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.Unmarshal(b, &auth)
	code, b = c.do("setup", "POST", "/v1/children", auth.AccessToken, k.ip,
		map[string]any{"display_name": "Kid", "grade": 3, "avatar_id": "fox", "pin": "1234"}, false)
	if code != 201 {
		return nil, fmt.Errorf("create child %d: status %d %s", i, code, b)
	}
	var ch struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(b, &ch)
	k.childID = ch.ID
	code, b = c.do("setup", "POST", "/v1/auth/consent", auth.AccessToken, k.ip,
		map[string]any{"child_id": k.childID, "version": "2026-10", "accepted": true}, false)
	if code != 204 {
		return nil, fmt.Errorf("consent %d: status %d %s", i, code, b)
	}
	code, b = c.do("setup", "POST", "/v1/children/"+k.childID+"/token", auth.AccessToken, k.ip,
		map[string]any{"pin": "1234"}, false)
	if code != 200 {
		return nil, fmt.Errorf("child token %d: status %d %s", i, code, b)
	}
	var ct struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.Unmarshal(b, &ct)
	k.token = ct.AccessToken
	return k, nil
}

// step performs the next action of a kid: start a session, ask a beat, or answer it.
func (c *client) step(k *kid) {
	defer k.busy.Store(false) // free the kid when the action ends
	switch {
	case k.sessionID == "": // no session yet: start one
		code, b := c.do("startSession", "POST", "/v1/children/"+k.childID+"/sessions", k.token, k.ip,
			map[string]any{"world": "nature", "kind": "chapter"}, true)
		if code == 201 || code == 200 {
			var s struct {
				ID string `json:"id"`
			}
			_ = json.Unmarshal(b, &s)
			k.sessionID = s.ID
		}
	case k.beatID == "": // session open, no beat: ask one
		code, b := c.do("nextBeat", "POST", "/v1/sessions/"+k.sessionID+"/beats", k.token, k.ip, nil, true)
		if code == 201 || code == 200 {
			var beat struct {
				ID       string `json:"id"`
				Question struct {
					Choices []json.RawMessage `json:"choices"`
				} `json:"question"`
			}
			_ = json.Unmarshal(b, &beat)
			k.beatID, k.choices = beat.ID, beat.Question.Choices
		} else if code != 429 && code != 0 { // session over or broken: begin a new one next time
			k.sessionID = ""
		}
	default: // beat open: answer with a random choice (the client does not know the right one)
		var ans json.RawMessage = json.RawMessage("0")
		if len(k.choices) > 0 {
			ans = k.choices[mrand.IntN(len(k.choices))]
		}
		code, b := c.do("answerBeat", "POST", "/v1/sessions/"+k.sessionID+"/beats/"+k.beatID+"/answer", k.token, k.ip,
			map[string]any{"answer": ans, "response_time_ms": 2000}, true)
		switch {
		case code == 200:
			k.beatID = ""
			var r struct {
				Session struct {
					Status string `json:"status"`
				} `json:"session"`
			}
			_ = json.Unmarshal(b, &r)
			if r.Session.Status != "active" { // chapter finished: next action starts a new session
				k.sessionID = ""
			}
		case code != 429 && code != 0: // 4xx/5xx other than throttling: drop the state and start over
			k.beatID, k.sessionID = "", ""
		}
	}
}

// stage is one segment of the load profile.
type stage struct {
	rps float64       // target requests per second
	dur time.Duration // how long
}

// parseStages turns "20:5s,300:20s" into stages.
func parseStages(s string) ([]stage, error) {
	var out []stage
	for _, part := range strings.Split(s, ",") {
		a, b, ok := strings.Cut(part, ":")
		if !ok {
			return nil, fmt.Errorf("bad stage %q, want rps:duration", part)
		}
		r, err := strconv.ParseFloat(a, 64)
		if err != nil || r <= 0 {
			return nil, fmt.Errorf("bad rps in %q", part)
		}
		d, err := time.ParseDuration(b)
		if err != nil {
			return nil, fmt.Errorf("bad duration in %q", part)
		}
		out = append(out, stage{r, d})
	}
	return out, nil
}

// pct returns the p-th percentile (0-100) of sorted durations.
func pct(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	i := int(float64(len(sorted)-1) * p / 100)
	return sorted[i]
}

// report prints the table and returns true when every budget holds.
func report(rec *recorder, elapsed time.Duration, skipped int64) bool {
	budget := map[string]time.Duration{ // contract section 24 p95 budgets
		"health": 100 * time.Millisecond, "nextBeat": 800 * time.Millisecond,
		"answerBeat": 400 * time.Millisecond, "startSession": 800 * time.Millisecond,
	}
	names := make([]string, 0, len(rec.m))
	for n := range rec.m {
		names = append(names, n)
	}
	sort.Strings(names)
	ok := true
	total, bad5xx, bad429 := 0, 0, 0
	fmt.Printf("%-13s %7s %8s %8s %8s %8s %6s %6s %6s  %s\n", "endpoint", "count", "p50", "p95", "p99", "max", "429", "5xx", "err", "budget p95")
	for _, n := range names {
		ss := rec.m[n]
		ds := make([]time.Duration, 0, len(ss))
		var c429, c5xx, cerr int
		for _, s := range ss {
			ds = append(ds, s.dur)
			switch {
			case s.status == 429:
				c429++
			case s.status >= 500:
				c5xx++
			case s.status == 0:
				cerr++
			}
		}
		sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
		verdict := "-"
		if b, has := budget[n]; has {
			verdict = "PASS"
			if pct(ds, 95) > b {
				verdict, ok = "FAIL", false
			}
			verdict += fmt.Sprintf(" (<%dms)", b.Milliseconds())
		}
		if c5xx > 0 || cerr > 0 {
			ok = false
		}
		if n == "tcp-connect" || n == "client-stall" { // diagnostics about the load generator, not API requests
			fmt.Printf("%-13s %7d %8s %8s %8s %8s\n", n, len(ss), pct(ds, 50).Round(100*time.Microsecond),
				pct(ds, 95).Round(100*time.Microsecond), pct(ds, 99).Round(100*time.Microsecond), ds[len(ds)-1].Round(100*time.Microsecond))
			continue
		}
		total += len(ss)
		bad5xx += c5xx + cerr
		bad429 += c429
		fmt.Printf("%-13s %7d %8s %8s %8s %8s %6d %6d %6d  %s\n", n, len(ss),
			pct(ds, 50).Round(100*time.Microsecond), pct(ds, 95).Round(100*time.Microsecond),
			pct(ds, 99).Round(100*time.Microsecond), ds[len(ds)-1].Round(100*time.Microsecond), c429, c5xx, cerr, verdict)
	}
	fmt.Printf("total=%d achieved=%.1f req/s 429=%d 5xx+conn-errors=%d client-skipped(no idle kid)=%d\n",
		total, float64(total)/elapsed.Seconds(), bad429, bad5xx, skipped)
	return ok
}

func main() {
	base := flag.String("base", "http://localhost:8090", "API base URL")
	rps := flag.Float64("rps", 50, "target requests per second")
	dur := flag.Duration("duration", 60*time.Second, "test duration")
	stagesF := flag.String("stages", "", `optional profile "rps:dur,rps:dur" (overrides -rps/-duration), e.g. "20:5s,300:20s,20:5s"`)
	families := flag.Int("families", 300, "number of families (one kid each)")
	xff := flag.Bool("xff", true, "send a distinct X-Forwarded-For per family (server needs TRUST_PROXY=true)")
	flag.Parse()

	stages := []stage{{*rps, *dur}} // default: a single stage
	if *stagesF != "" {
		var err error
		if stages, err = parseStages(*stagesF); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	}
	rec := &recorder{m: map[string][]sample{}}
	c := &client{base: *base, rec: rec, xff: *xff, http: &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{MaxIdleConnsPerHost: 1024, MaxConnsPerHost: 0,
			// time every new TCP connection: slow connects (SYN retries) would look like server latency otherwise
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				t := time.Now()
				conn, err := (&net.Dialer{}).DialContext(ctx, network, addr)
				rec.add("tcp-connect", 200, time.Since(t))
				return conn, err
			}},
	}}

	// Setup: families in parallel (16 at a time, argon2 hashing is CPU heavy on the server).
	nonce := strconv.FormatInt(time.Now().UnixNano(), 36)
	kids := make([]*kid, *families)
	var wg sync.WaitGroup
	sem := make(chan struct{}, 16) // counting semaphore
	var setupErr atomic.Value
	t0 := time.Now()
	for i := range kids {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			k, err := c.setupFamily(i, nonce)
			if err != nil {
				setupErr.Store(err)
				return
			}
			kids[i] = k
		}()
	}
	wg.Wait()
	if e := setupErr.Load(); e != nil {
		fmt.Fprintln(os.Stderr, "setup failed:", e)
		os.Exit(1)
	}
	fmt.Printf("setup: %d families in %s\n", *families, time.Since(t0).Round(time.Millisecond))

	// Watchdog: a goroutine that sleeps 10 ms; if it wakes late the load generator itself was starved,
	// so the latencies of that moment belong to the client machine, not to the server.
	go func() {
		for {
			t := time.Now()
			time.Sleep(10 * time.Millisecond)
			if d := time.Since(t); d > 200*time.Millisecond {
				rec.add("client-stall", 200, d)
			}
		}
	}()

	// Open-loop generator: one tick per request. Every 10th tick is a health probe, the rest are kid actions.
	var skipped atomic.Int64
	var inflight sync.WaitGroup
	start := time.Now()
	n, cursor := 0, 0
	for _, st := range stages {
		stageEnd := time.Now().Add(st.dur)
		interval := time.Duration(float64(time.Second) / st.rps)
		next := time.Now()
		for time.Now().Before(stageEnd) {
			if d := time.Until(next); d > 0 {
				time.Sleep(d) // wait for the clock, not for earlier requests
			}
			next = next.Add(interval)
			n++
			inflight.Add(1)
			if n%10 == 0 {
				go func() { defer inflight.Done(); c.do("health", "GET", "/v1/health", "", "", nil, true) }()
				continue
			}
			var k *kid // find the next idle kid, round robin
			for tries := 0; tries < len(kids); tries++ {
				cand := kids[cursor%len(kids)]
				cursor++
				if cand.busy.CompareAndSwap(false, true) {
					k = cand
					break
				}
			}
			if k == nil { // every kid is waiting on the server: the client cannot offer more load
				skipped.Add(1)
				inflight.Done()
				continue
			}
			go func() { defer inflight.Done(); c.step(k) }()
		}
	}
	inflight.Wait()
	if !report(rec, time.Since(start), skipped.Load()) {
		os.Exit(1) // non-zero exit so scripts can gate on it
	}
}
