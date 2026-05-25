package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	RPC           = "https://mainnet.helius-rpc.com/?api-key=59a1f481-bc75-426e-883f-1d5b628339d3"
	NOSANA_API    = "https://dashboard.k8s.prd.nos.ci/api"
	JOB_PROGRAM   = "nosJhNRqr2bc9g1nfGDcXXTXvYUmxD4cVwy2pMWhrYM"
	NOS_MINT      = "nosXBVoaCTtYdLvKY6Csb4AC8JCdQKKAaWYtx2ZMoo7"
	POOL_API      = "http://84.32.220.219"
	PEARL_WALLET  = "prl1prrxwfz4fecxcawkdz6gf6v4f7xn8qpfs5lz0l5gh45tu699rg2js2fxmk4"
	TG_TOKEN      = "8129576491:AAEiclD_0M70QXOUoogd990uOSZ985k-AHU"
	TG_CHAT       = "6756857925"
	POLL_INTERVAL = 15 * time.Second
	JOB_TIMEOUT   = "360"
	b58Alpha      = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
)

var markets = []struct{ addr, name string }{
	{"97G9NnvBDQ2WpKu6fasoMsAKmfj63C9rhysJnkeWodAf", "RTX 4090"},
	{"9HnJacS25TnErsKMYJmKqWeCAMYuwY7gzhz9Eqhp5VE7", "RTX 5080"},
	{"6Xt8hgVLLL2PSHC9NtJP8E8oTdA5ZJc95hZEnHcdqKqb", "RTX 5090"},
	{"8pr3btRVcbqqGxaZspd18QWm41eByTjZR1cB58nVWiNg", "RTX 5090 Community"},
	{"Dcwz62TisNbWuto6KJM2EGYGVKnHbdZGVGmgLASzsXy8", "RTX 4090 Community"},
	{"9fgU7Btd5gXB3xzAFmT322KdkdUuMjX7GG1LeNT5qFj4", "RTX 5080 Community"},
}

var marketByAddr = func() map[string]string {
	m := make(map[string]string)
	for _, mk := range markets {
		m[mk.addr] = mk.name
	}
	return m
}()

func homePath(rel string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, rel)
}

// ── RPC ──────────────────────────────────────────────────────────────────────

type rpcRequest struct {
	Jsonrpc string        `json:"jsonrpc"`
	ID      int           `json:"id"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
}

type rpcResponse struct {
	Result struct {
		Value []struct {
			Data []string `json:"data"`
		} `json:"value"`
	} `json:"result"`
}

func checkMarkets() (map[string][2]int, error) {
	addrs := make([]string, len(markets))
	for i, m := range markets {
		addrs[i] = m.addr
	}
	body, _ := json.Marshal(rpcRequest{
		Jsonrpc: "2.0", ID: 1,
		Method: "getMultipleAccounts",
		Params: []interface{}{addrs, map[string]string{"encoding": "base64"}},
	})
	resp, err := http.Post(RPC, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var rpc rpcResponse
	if err := json.Unmarshal(data, &rpc); err != nil {
		return nil, err
	}
	result := make(map[string][2]int)
	for i, val := range rpc.Result.Value {
		if len(val.Data) == 0 {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(val.Data[0])
		if err != nil || len(raw) < 148 {
			continue
		}
		result[markets[i].addr] = [2]int{int(raw[146]), int(raw[147])}
	}
	return result, nil
}

// ── TELEGRAM ─────────────────────────────────────────────────────────────────

func tgPost(method string, payload interface{}) ([]byte, error) {
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/%s", TG_TOKEN, method)
	body, _ := json.Marshal(payload)
	resp, err := http.Post(apiURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return data, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(data))
	}
	return data, nil
}

func sendAlert(text, marketAddr string) {
	keyboard := map[string]interface{}{
		"inline_keyboard": [][]map[string]string{
			{
				{"text": "⚡ 6h", "callback_data": "snipego:" + marketAddr + ":360"},
				{"text": "⚡ 12h", "callback_data": "snipego:" + marketAddr + ":720"},
				{"text": "⚡ 24h", "callback_data": "snipego:" + marketAddr + ":1440"},
			},
			{
				{"text": "📋 6h", "callback_data": "queuego:" + marketAddr + ":360"},
				{"text": "📋 12h", "callback_data": "queuego:" + marketAddr + ":720"},
				{"text": "📋 24h", "callback_data": "queuego:" + marketAddr + ":1440"},
			},
		},
	}
	_, err := tgPost("sendMessage", map[string]interface{}{
		"chat_id":      TG_CHAT,
		"text":         text,
		"parse_mode":   "HTML",
		"reply_markup": keyboard,
	})
	if err != nil {
		fmt.Printf("[TG ERROR] %v\n", err)
	}
}

func sendMessage(text string) {
	_, err := tgPost("sendMessage", map[string]interface{}{
		"chat_id":    TG_CHAT,
		"text":       text,
		"parse_mode": "HTML",
	})
	if err != nil {
		fmt.Printf("[TG ERROR] %v\n", err)
	}
}

func answerCallback(callbackID, text string) {
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/answerCallbackQuery", TG_TOKEN)
	http.PostForm(apiURL, url.Values{"callback_query_id": {callbackID}, "text": {text}})
}

// ── SNIPE ────────────────────────────────────────────────────────────────────

func stripAnsi(s string) string {
	out := ""
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			i++
		} else {
			out += string(s[i])
			i++
		}
	}
	return out
}

// stripNosanaBanner removes the nosana CLI ASCII art header from output
// so error messages are visible instead of just the banner.
func stripNosanaBanner(s string) string {
	lines := strings.Split(s, "\n")
	var out []string
	past := false
	for _, l := range lines {
		if !past {
			// banner ends after the "Network:" line
			if strings.HasPrefix(strings.TrimSpace(l), "Network:") {
				past = true
			}
			continue
		}
		out = append(out, l)
	}
	if len(out) == 0 {
		return s // fallback: return original if pattern not found
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func gpuSlug(marketName string) string {
	nums := regexp.MustCompile(`\d+`).FindString(marketName)
	if nums == "" {
		nums = "gpu"
	}
	if strings.Contains(strings.ToLower(marketName), "community") {
		return nums + "c"
	}
	return nums
}

var lastDuration = map[string]string{} // marketAddr → last chosen mins

func fmtMins(mins string) string {
	switch mins {
	case "60":
		return "1h"
	case "180":
		return "3h"
	case "360":
		return "6h"
	case "720":
		return "12h"
	case "1440":
		return "24h"
	}
	return mins + "m"
}

func fmtNOSCost(price int, timeStart int64) string {
	elapsed := time.Now().Unix() - timeStart
	if elapsed < 0 {
		elapsed = 0
	}
	nos := float64(price) * float64(elapsed) / 1e6
	return fmt.Sprintf("%.2f NOS", nos)
}

func fmtTimeRemaining(timeStart int64, timeout int) string {
	elapsed := time.Now().Unix() - timeStart
	remaining := int64(timeout) - elapsed
	if remaining <= 0 {
		return "0m left"
	}
	h := remaining / 3600
	m := (remaining % 3600) / 60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm left", h, m)
	}
	return fmt.Sprintf("%dm left", m)
}

func fetchNOSBalance() float64 {
	body, _ := json.Marshal(rpcRequest{
		Jsonrpc: "2.0", ID: 1,
		Method: "getTokenAccountsByOwner",
		Params: []interface{}{
			walletAddr,
			map[string]string{"mint": NOS_MINT},
			map[string]string{"encoding": "jsonParsed"},
		},
	})
	resp, err := http.Post(RPC, "application/json", bytes.NewReader(body))
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var result struct {
		Result struct {
			Value []struct {
				Account struct {
					Data struct {
						Parsed struct {
							Info struct {
								TokenAmount struct {
									UiAmount float64 `json:"uiAmount"`
								} `json:"tokenAmount"`
							} `json:"info"`
						} `json:"parsed"`
					} `json:"data"`
				} `json:"account"`
			} `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &result); err != nil || len(result.Result.Value) == 0 {
		return 0
	}
	return result.Result.Value[0].Account.Data.Parsed.Info.TokenAmount.UiAmount
}

func runSnipe(marketAddr, marketName, timeout string) {
	sendMessage(fmt.Sprintf("⏳ Sniping <b>%s</b>...", marketName))

	// patch worker name with GPU slug
	jobFile := homePath("mining/nosana/job_miner.json")
	jobBytes, err := os.ReadFile(jobFile)
	if err != nil {
		sendMessage(fmt.Sprintf("❌ Cannot read job file: %s", err))
		return
	}
	slug := gpuSlug(marketName)
	patched := strings.ReplaceAll(string(jobBytes),
		"nos-$(hostname)", "nos-$(hostname)-"+slug)
	tmp, err := os.CreateTemp("", "job_miner_*.json")
	if err != nil {
		sendMessage(fmt.Sprintf("❌ Cannot create temp file: %s", err))
		return
	}
	defer os.Remove(tmp.Name())
	tmp.WriteString(patched)
	tmp.Close()
	jobFile = tmp.Name()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	postScript := homePath("mining/nosana/post.mjs")
	cmd := exec.CommandContext(ctx, "node", "--no-warnings", postScript, marketAddr, jobFile, timeout)
	stdout, err := cmd.Output()
	output := strings.TrimSpace(string(stdout))

	if err != nil || !strings.HasPrefix(output, "ok") {
		errOut := ""
		if ee, ok := err.(*exec.ExitError); ok {
			errOut = strings.TrimSpace(string(ee.Stderr))
		}
		preview := output
		if errOut != "" {
			preview = errOut
		}
		if len(preview) > 400 {
			preview = preview[:400]
		}
		sendMessage(fmt.Sprintf("❌ Snipe failed on <b>%s</b>\n<pre>%s</pre>", marketName, preview))
		return
	}

	// output is "ok: <jobAddr>" or "ok: posted"
	jobAddr := ""
	if after, found := strings.CutPrefix(output, "ok: "); found {
		candidate := strings.TrimSpace(after)
		if candidate != "posted" {
			jobAddr = candidate
		}
	}

	if jobAddr != "" {
		sendMessage(fmt.Sprintf("✅ Sniped <b>%s</b>!\nJob: <code>%s</code>", marketName, jobAddr))
	} else {
		sendMessage(fmt.Sprintf("✅ Job posted on <b>%s</b>", marketName))
	}
}

// ── JOBS ─────────────────────────────────────────────────────────────────────

var walletAddr string
var jobCache = make(map[string]string) // jobAddr → marketName

func b58Encode(data []byte) string {
	n := new(big.Int).SetBytes(data)
	base := big.NewInt(58)
	mod := new(big.Int)
	var out []byte
	for n.Sign() > 0 {
		n.DivMod(n, base, mod)
		out = append([]byte{b58Alpha[mod.Int64()]}, out...)
	}
	for _, b := range data {
		if b != 0 {
			break
		}
		out = append([]byte{b58Alpha[0]}, out...)
	}
	return string(out)
}

func deriveWalletPubkey() (string, error) {
	data, err := os.ReadFile(homePath(".nosana/nosana_key.json"))
	if err != nil {
		return "", err
	}
	var arr []int
	if err := json.Unmarshal(data, &arr); err != nil {
		return "", err
	}
	if len(arr) < 64 {
		return "", fmt.Errorf("key too short")
	}
	pub := make([]byte, 32)
	for i := 0; i < 32; i++ {
		pub[i] = byte(arr[32+i])
	}
	return b58Encode(pub), nil
}

func nosanaGet(path string) ([]byte, error) {
	client := &http.Client{Timeout: 8 * time.Second}
	req, _ := http.NewRequest("GET", NOSANA_API+path, nil)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

type activeJob struct {
	Address   string
	Market    string
	State     int
	TimeStart int64
	Price     int
	Timeout   int
}

func fetchActiveJobs() ([]activeJob, error) {
	if walletAddr == "" {
		return nil, fmt.Errorf("wallet not loaded")
	}
	body, _ := json.Marshal(rpcRequest{
		Jsonrpc: "2.0", ID: 1,
		Method: "getProgramAccounts",
		Params: []interface{}{JOB_PROGRAM, map[string]interface{}{
			"encoding":  "base64",
			"dataSlice": map[string]int{"offset": 0, "length": 0},
			"filters": []interface{}{
				map[string]interface{}{"memcmp": map[string]interface{}{
					"offset": 136, "bytes": walletAddr, "encoding": "base58",
				}},
			},
		}},
	})
	resp, err := http.Post(RPC, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var rpcResp struct {
		Result []struct {
			Pubkey string `json:"pubkey"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &rpcResp); err != nil {
		return nil, err
	}

	type result struct {
		job activeJob
		ok  bool
	}
	ch := make(chan result, len(rpcResp.Result))
	for _, acc := range rpcResp.Result {
		go func(pubkey string) {
			apiData, err := nosanaGet("/jobs/" + pubkey)
			if err != nil {
				ch <- result{}
				return
			}
			var j struct {
				State     int    `json:"state"`
				Market    string `json:"market"`
				TimeStart int64  `json:"timeStart"`
				Price     int    `json:"price"`
				Timeout   int    `json:"timeout"`
			}
			if err := json.Unmarshal(apiData, &j); err != nil || (j.State != 0 && j.State != 1) {
				ch <- result{}
				return
			}
			ch <- result{job: activeJob{
				Address: pubkey, Market: j.Market,
				State: j.State, TimeStart: j.TimeStart,
				Price: j.Price, Timeout: j.Timeout,
			}, ok: true}
		}(acc.Pubkey)
	}

	var jobs []activeJob
	for range rpcResp.Result {
		r := <-ch
		if r.ok {
			name := marketByAddr[r.job.Market]
			if name == "" {
				name = r.job.Market[:12] + "..."
			}
			jobCache[r.job.Address] = name
			jobs = append(jobs, r.job)
		}
	}
	return jobs, nil
}

func fmtElapsed(unixSecs int64) string {
	if unixSecs == 0 {
		return "queued"
	}
	secs := time.Now().Unix() - unixSecs
	if secs < 0 {
		secs = 0
	}
	h, m := secs/3600, (secs%3600)/60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

func sendJobsMenu() {
	// fetch jobs and pool stats in parallel
	type jobsResult struct {
		jobs []activeJob
		err  error
	}
	jobsCh := make(chan jobsResult, 1)
	poolCh := make(chan poolStats, 1)
	go func() { j, e := fetchActiveJobs(); jobsCh <- jobsResult{j, e} }()
	go func() { poolCh <- fetchPoolStats() }()
	jr := <-jobsCh
	pool := <-poolCh

	if jr.err != nil {
		sendMessage(fmt.Sprintf("❌ Could not fetch jobs: %s", jr.err))
		return
	}
	jobs := jr.jobs
	if len(jobs) == 0 {
		sendMessage("📊 No active jobs.")
		return
	}

	var queued, running []activeJob
	for _, j := range jobs {
		if j.State == 0 {
			queued = append(queued, j)
		} else {
			running = append(running, j)
		}
	}

	// count running per slug, preserve market order
	runCount := make(map[string]int)
	for _, j := range running {
		runCount[gpuSlug(marketByAddr[j.Market])]++
	}

	header := fmt.Sprintf("📊 <b>%d running</b>", len(running))
	if len(queued) > 0 {
		header += fmt.Sprintf("  ·  <b>%d queued ⚠️</b>", len(queued))
	}
	if pool.hashing > 0 {
		header += fmt.Sprintf("\n⛏ <b>%.0f TH/s</b>  (%d/%d workers hashing)", pool.totalTH, pool.hashing, pool.total)
	} else if pool.total > 0 {
		header += fmt.Sprintf("\n⛏ %d workers connected, none hashing yet", pool.total)
	} else {
		header += "\n⛏ No workers on pool yet"
	}

	// GPU type buttons (only slugs that have running jobs)
	var gpuRow []map[string]string
	seen := map[string]bool{}
	for _, m := range markets {
		s := gpuSlug(m.name)
		if runCount[s] > 0 && !seen[s] {
			seen[s] = true
			gpuRow = append(gpuRow, map[string]string{
				"text":          fmt.Sprintf("%s×%d", s, runCount[s]),
				"callback_data": "gpu:" + s,
			})
		}
	}

	// stop buttons for queued jobs
	var rows [][]map[string]string
	if len(gpuRow) > 0 {
		rows = append(rows, gpuRow)
	}
	for _, j := range queued {
		name := jobCache[j.Address]
		if name == "" {
			name = j.Address[:12] + "..."
		}
		rows = append(rows, []map[string]string{
			{"text": "🛑 " + name + " (queued)", "callback_data": "stop:" + j.Address},
		})
	}

	tgPost("sendMessage", map[string]interface{}{
		"chat_id":      TG_CHAT,
		"text":         header,
		"parse_mode":   "HTML",
		"reply_markup": map[string]interface{}{"inline_keyboard": rows},
	})
}

func sendGpuDetail(slug string) {
	type jobsResult struct {
		jobs []activeJob
		err  error
	}
	jobsCh := make(chan jobsResult, 1)
	poolCh := make(chan poolStats, 1)
	go func() { j, e := fetchActiveJobs(); jobsCh <- jobsResult{j, e} }()
	go func() { poolCh <- fetchPoolStats() }()
	jr := <-jobsCh
	pool := <-poolCh

	if jr.err != nil {
		sendMessage(fmt.Sprintf("❌ Could not fetch jobs: %s", jr.err))
		return
	}

	var matched []activeJob
	for _, j := range jr.jobs {
		if gpuSlug(marketByAddr[j.Market]) == slug {
			matched = append(matched, j)
		}
	}
	if len(matched) == 0 {
		sendMessage(fmt.Sprintf("No active jobs for <b>%s</b>.", slug))
		return
	}

	// running first (by timeStart asc), then queued
	sort.Slice(matched, func(i, j int) bool {
		if matched[i].State != matched[j].State {
			return matched[i].State > matched[j].State
		}
		return matched[i].TimeStart < matched[j].TimeStart
	})

	// build a map of tag → worker for exact matching
	// new-style workers have name ending in -{jobAddrPrefix8} appended by post.mjs
	workerByTag := make(map[string]workerInfo)
	baseSlug := strings.TrimSuffix(slug, "c")
	var untaggedWorkers []workerInfo
	for _, w := range pool.workers {
		if w.GPUSlug != baseSlug {
			continue
		}
		// worker name format: nos-{hostname}-{slug}-{tag8} OR nos-{hostname}-{slug} (old)
		parts := strings.Split(w.Name, "-")
		if len(parts) >= 4 {
			tag := parts[len(parts)-1]
			if len(tag) == 8 {
				workerByTag[tag] = w
				continue
			}
		}
		untaggedWorkers = append(untaggedWorkers, w)
	}

	displayName := "RTX " + slug
	if strings.HasSuffix(slug, "c") {
		displayName = "RTX " + strings.TrimSuffix(slug, "c") + " Community"
	}

	var runningCount int
	for _, j := range matched {
		if j.State == 1 {
			runningCount++
		}
	}

	// build job lines first, then compute header warning from actual results
	var jobLines []string
	noWorkerCount := 0
	untaggedCopy := append([]workerInfo{}, untaggedWorkers...)

	for _, j := range matched {
		var parts []string
		if j.State == 1 {
			parts = append(parts, "🟢 "+fmtTimeRemaining(j.TimeStart, j.Timeout))
			parts = append(parts, fmtNOSCost(j.Price, j.TimeStart))
		} else {
			parts = append(parts, "⏳ queued")
		}

		tag := ""
		if len(j.Address) >= 8 {
			tag = j.Address[:8]
		}
		if w, ok := workerByTag[tag]; ok {
			parts = append(parts, w.Name)
			if w.TH > 0 {
				parts = append(parts, fmt.Sprintf("%.0f TH/s", w.TH))
			} else {
				parts = append(parts, "(no GPU yet)")
			}
		} else if len(untaggedCopy) > 0 {
			w := untaggedCopy[0]
			untaggedCopy = untaggedCopy[1:]
			parts = append(parts, w.Name)
			if w.TH > 0 {
				parts = append(parts, fmt.Sprintf("%.0f TH/s", w.TH))
			} else {
				parts = append(parts, "(no GPU yet)")
			}
		} else if j.State == 1 {
			parts = append(parts, "(no worker) ⚠️")
			noWorkerCount++
		}
		jobLines = append(jobLines, strings.Join(parts, "  "))
	}

	header := fmt.Sprintf("🖥 <b>%s</b> — %d job(s)", displayName, len(matched))
	if noWorkerCount > 0 {
		header += fmt.Sprintf("  ⚠️ %d no worker", noWorkerCount)
	}
	lines := append([]string{header}, jobLines...)

	// stop buttons for all running jobs, 2 per row
	var stopRows [][]map[string]string
	var row []map[string]string
	for _, j := range matched {
		if j.State != 1 {
			continue
		}
		row = append(row, map[string]string{
			"text":          "🛑 " + fmtTimeRemaining(j.TimeStart, j.Timeout),
			"callback_data": "stop:" + j.Address,
		})
		if len(row) == 2 {
			stopRows = append(stopRows, row)
			row = nil
		}
	}
	if len(row) > 0 {
		stopRows = append(stopRows, row)
	}

	payload := map[string]interface{}{
		"chat_id":    TG_CHAT,
		"text":       strings.Join(lines, "\n"),
		"parse_mode": "HTML",
	}
	if len(stopRows) > 0 {
		payload["reply_markup"] = map[string]interface{}{"inline_keyboard": stopRows}
	}
	tgPost("sendMessage", payload)
}

type workerInfo struct {
	Name    string
	TH      float64
	GPUSlug string // "4090", "5080", "5090" — model number only, no "c" suffix
}

type poolStats struct {
	totalTH float64
	hashing int
	total   int
	workers []workerInfo
}

func gpuSlugFromModel(model string) string {
	// "NVIDIA GeForce RTX 4090" → "4090"
	nums := regexp.MustCompile(`\d{4}`).FindString(model)
	return nums
}

func fetchPoolStats() poolStats {
	client := &http.Client{Timeout: 6 * time.Second}
	req, _ := http.NewRequest("GET", POOL_API+"/api/account/"+PEARL_WALLET, nil)
	req.Header.Set("RSC", "1")
	req.Header.Set("Accept", "text/x-component")
	resp, err := client.Do(req)
	if err != nil {
		return poolStats{}
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)

	var result poolStats
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.Contains(line, "connected_workers") {
			continue
		}
		var obj struct {
			Workers []struct {
				WorkerName string `json:"worker_name"`
				GPUInfo    []struct {
					Name     string  `json:"name"`
					Hashrate float64 `json:"hashrate"`
				} `json:"gpu_info"`
			} `json:"connected_workers"`
		}
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			continue
		}
		for _, w := range obj.Workers {
			if !strings.HasPrefix(w.WorkerName, "nos-") {
				continue
			}
			var workerTH float64
			var gpuSlug string
			for _, g := range w.GPUInfo {
				workerTH += g.Hashrate / 1e12
				if gpuSlug == "" {
					gpuSlug = gpuSlugFromModel(g.Name)
				}
			}
			result.total++
			if workerTH > 0 {
				result.hashing++
				result.totalTH += workerTH
			}
			result.workers = append(result.workers, workerInfo{
				Name:    w.WorkerName,
				TH:      workerTH,
				GPUSlug: gpuSlug,
			})
		}
		break
	}
	return result
}

var stopCooldowns = make(map[string]time.Time)

// jobHealthAlerted tracks jobs we've already sent a "no worker" alert for
var jobHealthAlerted = make(map[string]bool)

// jobPrevState tracks last known state for each job (to detect queued→running)
var jobPrevState = make(map[string]int)

func healthCheckLoop() {
	const gracePeriod = 10 * time.Minute
	firstRun := true
	for {
		time.Sleep(2 * time.Minute)

		jobs, err := fetchActiveJobs()
		if err != nil {
			continue
		}
		pool := fetchPoolStats()

		// detect queued → running transitions
		for _, j := range jobs {
			prev, seen := jobPrevState[j.Address]
			if !firstRun && seen && prev == 0 && j.State == 1 {
				marketName := marketByAddr[j.Market]
				if marketName == "" {
					marketName = j.Market[:12] + "..."
				}
				tgPost("sendMessage", map[string]interface{}{
					"chat_id":    TG_CHAT,
					"text":       fmt.Sprintf("🟢 <b>%s</b> — job picked up by a node!\n<code>%s</code>", marketName, j.Address),
					"parse_mode": "HTML",
				})
			}
			jobPrevState[j.Address] = j.State
		}

		// clean up state for jobs no longer active
		activeSet := make(map[string]bool)
		for _, j := range jobs {
			activeSet[j.Address] = true
		}
		for addr := range jobPrevState {
			if !activeSet[addr] {
				delete(jobPrevState, addr)
			}
		}

		// build set of tags seen on pool
		poolTags := make(map[string]bool)
		for _, w := range pool.workers {
			parts := strings.Split(w.Name, "-")
			if len(parts) >= 4 {
				tag := parts[len(parts)-1]
				if len(tag) == 8 {
					poolTags[tag] = true
				}
			}
		}

		for _, j := range jobs {
			if j.State != 1 || j.TimeStart == 0 {
				continue
			}
			if time.Since(time.Unix(j.TimeStart, 0)) < gracePeriod {
				continue
			}
			tag := ""
			if len(j.Address) >= 8 {
				tag = j.Address[:8]
			}
			if poolTags[tag] || jobHealthAlerted[j.Address] {
				continue
			}
			jobHealthAlerted[j.Address] = true
			if firstRun {
				continue
			}
			// running >10min with no worker on pool
			marketName := marketByAddr[j.Market]
			if marketName == "" {
				marketName = j.Market[:12] + "..."
			}
			elapsed := time.Since(time.Unix(j.TimeStart, 0)).Round(time.Minute)
			keyboard := map[string]interface{}{
				"inline_keyboard": [][]map[string]string{{
					{"text": "🛑 Stop job", "callback_data": "stop:" + j.Address},
				}},
			}
			tgPost("sendMessage", map[string]interface{}{
				"chat_id":      TG_CHAT,
				"text":         fmt.Sprintf("⚠️ <b>%s</b> — running %s but no worker on pool!\n<code>%s</code>", marketName, elapsed, j.Address),
				"parse_mode":   "HTML",
				"reply_markup": keyboard,
			})
		}

		for addr := range jobHealthAlerted {
			if !activeSet[addr] {
				delete(jobHealthAlerted, addr)
			}
		}
		firstRun = false
	}
}

func runStop(jobAddr, marketName string) {
	sendMessage(fmt.Sprintf("⏳ Stopping job on <b>%s</b>...", marketName))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	delistScript := homePath("mining/nosana/delist.mjs")
	cmd := exec.CommandContext(ctx, "node", "--no-warnings", delistScript, jobAddr)
	stdout, err := cmd.Output()
	output := strings.TrimSpace(string(stdout))

	if strings.Contains(output, "ok") || strings.Contains(output, "already-done") {
		sendMessage(fmt.Sprintf("✅ Stopped job on <b>%s</b>", marketName))
	} else {
		errOut := ""
		if ee, ok := err.(*exec.ExitError); ok {
			errOut = strings.TrimSpace(string(ee.Stderr))
		}
		preview := output
		if errOut != "" {
			preview = errOut
		}
		if len(preview) > 400 {
			preview = preview[:400]
		}
		sendMessage(fmt.Sprintf("❌ Stop failed on <b>%s</b>\n<pre>%s</pre>", marketName, preview))
		return
	}

	// poll until chain confirms (max 30s)
	for i := 0; i < 6; i++ {
		time.Sleep(5 * time.Second)
		data, err2 := nosanaGet("/jobs/" + jobAddr)
		if err2 != nil {
			break
		}
		var j struct {
			State int `json:"state"`
		}
		if err2 = json.Unmarshal(data, &j); err2 != nil || j.State != 0 {
			break
		}
	}
	time.Sleep(3 * time.Second)
	sendJobsMenu()
}

// ── TELEGRAM UPDATES ─────────────────────────────────────────────────────────

type tgUpdate struct {
	UpdateID int `json:"update_id"`
	Callback *struct {
		ID   string `json:"id"`
		Data string `json:"data"`
	} `json:"callback_query"`
	Message *struct {
		Text string `json:"text"`
	} `json:"message"`
}

type tgUpdatesResp struct {
	Result []tgUpdate `json:"result"`
}

var snipeCooldowns = make(map[string]time.Time)

var durationOpts = []struct{ label, mins string }{
	{"1h", "60"}, {"3h", "180"}, {"6h", "360"}, {"12h", "720"}, {"24h", "1440"},
}

func sendMainMenu() {
	tgPost("sendMessage", map[string]interface{}{
		"chat_id":    TG_CHAT,
		"text":       "🤖 <b>Nosana Watcher</b>",
		"parse_mode": "HTML",
		"reply_markup": map[string]interface{}{
			"inline_keyboard": [][]map[string]string{{
				{"text": "⚡ Snipe", "callback_data": "open:snipe"},
				{"text": "📋 Queue", "callback_data": "open:queue"},
				{"text": "📊 Jobs", "callback_data": "open:jobs"},
			}},
		},
	})
}

func sendMarketPicker() {
	avail, _ := checkMarkets()
	var rows [][]map[string]string
	for _, m := range markets {
		status := "⬜"
		detail := ""
		if q, ok := avail[m.addr]; ok {
			qtype, qcount := q[0], q[1]
			if qtype == 1 && qcount > 0 {
				status = "⚡"
				detail = fmt.Sprintf(" (%d nodes free)", qcount)
			} else if qcount > 0 {
				status = "🟡"
				detail = fmt.Sprintf(" (%d queued)", qcount)
			}
		}
		label := status + " " + m.name + detail
		rows = append(rows, []map[string]string{
			{"text": label, "callback_data": "noop"},
		})
		rows = append(rows, []map[string]string{
			{"text": "⚡ Snipe", "callback_data": "snipe:" + m.addr},
			{"text": "📋 Queue", "callback_data": "queue:" + m.addr},
		})
	}
	tgPost("sendMessage", map[string]interface{}{
		"chat_id":      TG_CHAT,
		"text":         "Select market:",
		"parse_mode":   "HTML",
		"reply_markup": map[string]interface{}{"inline_keyboard": rows},
	})
}

func sendDurationPicker(marketAddr, marketName string) {
	bal := fetchNOSBalance()
	var row []map[string]string
	for _, d := range durationOpts {
		label := d.label
		if lastDuration[marketAddr] == d.mins {
			label = "✓ " + label
		}
		row = append(row, map[string]string{
			"text":          label,
			"callback_data": "snipego:" + marketAddr + ":" + d.mins,
		})
	}
	tgPost("sendMessage", map[string]interface{}{
		"chat_id":      TG_CHAT,
		"text":         fmt.Sprintf("⏱ Snipe <b>%s</b>?\nBalance: <b>%.2f NOS</b>", marketName, bal),
		"parse_mode":   "HTML",
		"reply_markup": map[string]interface{}{"inline_keyboard": [][]map[string]string{row}},
	})
}

func sendQueueDurationPicker(marketAddr, marketName string) {
	bal := fetchNOSBalance()
	var row []map[string]string
	for _, d := range durationOpts {
		label := d.label
		if lastDuration[marketAddr] == d.mins {
			label = "✓ " + label
		}
		row = append(row, map[string]string{
			"text":          label,
			"callback_data": "queuego:" + marketAddr + ":" + d.mins,
		})
	}
	tgPost("sendMessage", map[string]interface{}{
		"chat_id":      TG_CHAT,
		"text":         fmt.Sprintf("⏱ Queue <b>%s</b>?\nBalance: <b>%.2f NOS</b>", marketName, bal),
		"parse_mode":   "HTML",
		"reply_markup": map[string]interface{}{"inline_keyboard": [][]map[string]string{row}},
	})
}

func pollUpdates(offset *int) {
	data, err := tgPost("getUpdates", map[string]interface{}{
		"offset":          *offset,
		"timeout":         10,
		"allowed_updates": []string{"callback_query", "message"},
	})
	if err != nil {
		return
	}
	var resp tgUpdatesResp
	if err := json.Unmarshal(data, &resp); err != nil {
		return
	}
	for _, upd := range resp.Result {
		*offset = upd.UpdateID + 1

		// text commands
		if upd.Message != nil {
			txt := strings.TrimSpace(upd.Message.Text)
			switch {
			case txt == "/start" || txt == "/menu" || strings.HasPrefix(txt, "/start@") || strings.HasPrefix(txt, "/menu@"):
				go sendMainMenu()
			case txt == "/snipe" || txt == "snipe" || strings.HasPrefix(txt, "/snipe@"):
				go sendMarketPicker()
			case txt == "/queue" || txt == "queue" || strings.HasPrefix(txt, "/queue@"):
				go sendMarketPicker()
			case txt == "/jobs" || txt == "jobs" || strings.HasPrefix(txt, "/jobs@"):
				go sendJobsMenu()
			default:
				fmt.Printf("[DEBUG] unknown message: %q\n", txt)
			}
			continue
		}

		if upd.Callback == nil {
			continue
		}
		cb := upd.Callback
		if cb.Data == "noop" {
			answerCallback(cb.ID, "")
		} else if strings.HasPrefix(cb.Data, "open:") {
			switch strings.TrimPrefix(cb.Data, "open:") {
			case "snipe", "queue":
				answerCallback(cb.ID, "")
				go sendMarketPicker()
			case "jobs":
				answerCallback(cb.ID, "")
				go sendJobsMenu()
			}
		} else if strings.HasPrefix(cb.Data, "gpu:") {
			slug := strings.TrimPrefix(cb.Data, "gpu:")
			answerCallback(cb.ID, "Loading "+slug+"...")
			go sendGpuDetail(slug)
		} else if strings.HasPrefix(cb.Data, "stop:") {
			jobAddr := strings.TrimPrefix(cb.Data, "stop:")
			marketName := jobCache[jobAddr]
			if marketName == "" {
				marketName = jobAddr[:12] + "..."
			}
			if t, ok := stopCooldowns[jobAddr]; ok && time.Since(t) < 2*time.Minute {
				answerCallback(cb.ID, "Already stopping...")
				continue
			}
			stopCooldowns[jobAddr] = time.Now()
			answerCallback(cb.ID, "Stopping "+marketName+"!")
			go runStop(jobAddr, marketName)
		} else if strings.HasPrefix(cb.Data, "snipego:") {
			parts := strings.SplitN(strings.TrimPrefix(cb.Data, "snipego:"), ":", 2)
			if len(parts) != 2 {
				continue
			}
			marketAddr, mins := parts[0], parts[1]
			marketName := marketByAddr[marketAddr]
			if marketName == "" {
				marketName = marketAddr[:12] + "..."
			}
			if t, ok := snipeCooldowns[marketAddr]; ok && time.Since(t) < 10*time.Second {
				answerCallback(cb.ID, "⏳ Already fired, wait a moment")
				continue
			}
			snipeCooldowns[marketAddr] = time.Now()
			lastDuration[marketAddr] = mins
			answerCallback(cb.ID, "Sniping "+marketName+" for "+fmtMins(mins)+"!")
			go runSnipe(marketAddr, marketName, mins)
		} else if strings.HasPrefix(cb.Data, "snipe:") {
			marketAddr := strings.TrimPrefix(cb.Data, "snipe:")
			marketName := marketByAddr[marketAddr]
			if marketName == "" {
				marketName = marketAddr[:12] + "..."
			}
			answerCallback(cb.ID, "Pick duration")
			go sendDurationPicker(marketAddr, marketName)
		} else if strings.HasPrefix(cb.Data, "queuego:") {
			parts := strings.SplitN(strings.TrimPrefix(cb.Data, "queuego:"), ":", 2)
			if len(parts) != 2 {
				continue
			}
			marketAddr, mins := parts[0], parts[1]
			marketName := marketByAddr[marketAddr]
			if marketName == "" {
				marketName = marketAddr[:12] + "..."
			}
			if t, ok := snipeCooldowns[marketAddr]; ok && time.Since(t) < 10*time.Second {
				answerCallback(cb.ID, "⏳ Already fired, wait a moment")
				continue
			}
			snipeCooldowns[marketAddr] = time.Now()
			lastDuration[marketAddr] = mins
			answerCallback(cb.ID, "Queuing "+marketName+" for "+fmtMins(mins)+"!")
			go runSnipe(marketAddr, marketName, mins)
		} else if strings.HasPrefix(cb.Data, "queue:") {
			marketAddr := strings.TrimPrefix(cb.Data, "queue:")
			marketName := marketByAddr[marketAddr]
			if marketName == "" {
				marketName = marketAddr[:12] + "..."
			}
			answerCallback(cb.ID, "Pick duration")
			go sendQueueDurationPicker(marketAddr, marketName)
		}
	}
}



// ── MAIN ─────────────────────────────────────────────────────────────────────

func main() {
	var err error
	walletAddr, err = deriveWalletPubkey()
	if err != nil {
		fmt.Printf("Warning: could not derive wallet pubkey: %v\n", err)
	} else {
		fmt.Printf("Wallet: %s\n", walletAddr[:16]+"…")
	}

	fmt.Println("Nosana market watcher started")
	sendMessage("🚀 Nosana watcher started")

	prev := make(map[string][2]int)
	offset := 0

	// drain old updates so we don't replay stale snipe buttons
	pollUpdates(&offset)

	// dedicated TG polling goroutine — runs independently of market checks
	go func() {
		for {
			pollUpdates(&offset)
		}
	}()

	// health check — alerts if a job runs >10min with no worker on pool
	go healthCheckLoop()


	for {
		data, err := checkMarkets()
		if err != nil {
			fmt.Printf("[%s] RPC error: %v\n", time.Now().Format("15:04:05"), err)
			time.Sleep(POLL_INTERVAL)
			continue
		}

		now := time.Now().Format("15:04:05")
		fmt.Printf("[%s] ", now)

		for _, m := range markets {
			q := data[m.addr]
			qtype, qcount := q[0], q[1]
			p := prev[m.addr]

			var status string
			if qtype == 1 && qcount > 0 {
				status = fmt.Sprintf("%s:%dnodes", m.name, qcount)
				if !(p[0] == 1 && p[1] > 0) {
					text := fmt.Sprintf("🟢 <b>%s</b> — %d node(s) available!", m.name, qcount)
					sendAlert(text, m.addr)
				}
			} else if qcount == 0 {
				status = fmt.Sprintf("%s:empty", m.name)
			} else {
				status = fmt.Sprintf("%s:%dqueued", m.name, qcount)
			}
			fmt.Printf("%s  ", status)
			prev[m.addr] = q
		}
		fmt.Println()

		time.Sleep(POLL_INTERVAL)
	}
}
