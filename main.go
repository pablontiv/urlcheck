package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

const version = "0.1.0"

type Status string

const (
	StatusOK      Status = "OK"
	StatusBlocked Status = "BLOCKED"
	StatusDown    Status = "DOWN"
	StatusError   Status = "ERROR"
)

type Service struct {
	Name string
	URL  string
}

type Result struct {
	Name       string        `json:"name"`
	URL        string        `json:"url"`
	Status     Status        `json:"status"`
	HTTPCode   int           `json:"http_code,omitempty"`
	Latency    time.Duration `json:"latency_ms"`
	ResolvedIP string        `json:"resolved_ip,omitempty"`
	Reason     string        `json:"reason"`
}

func (r Result) latencyMS() int64 {
	return r.Latency.Milliseconds()
}

var sinkholeIPs = map[string]bool{
	"127.0.0.1":  true,
	"0.0.0.0":    true,
	"::1":        true,
	"198.18.0.1": true, // dummy / filter frecuente
}

func main() {
	listPath := flag.String("list", "services.txt", "archivo plano con NAME URL (una por línea)")
	timeout := flag.Duration("timeout", 8*time.Second, "timeout por URL")
	workers := flag.Int("workers", 12, "comprobaciones en paralelo")
	jsonOut := flag.Bool("json", false, "salida JSON")
	insecure := flag.Bool("insecure", false, "ignorar errores de certificado TLS")
	method := flag.String("method", "HEAD", "método HTTP inicial (HEAD o GET)")
	showVer := flag.Bool("version", false, "mostrar versión")
	flag.Parse()

	if *showVer {
		fmt.Println("urlcheck", version)
		return
	}

	services, err := loadList(*listPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error leyendo %s: %v\n", *listPath, err)
		os.Exit(1)
	}
	if len(services) == 0 {
		fmt.Fprintf(os.Stderr, "no hay servicios en %s\n", *listPath)
		os.Exit(1)
	}

	client := &http.Client{
		Timeout: *timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: *insecure},
			Proxy:           http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   *timeout,
				KeepAlive: 0,
			}).DialContext,
			DisableKeepAlives:     true,
			TLSHandshakeTimeout:   *timeout,
			ResponseHeaderTimeout: *timeout,
			MaxIdleConns:          0,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 8 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}

	results := checkAll(services, client, *timeout, *workers, strings.ToUpper(*method))

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(results)
		return
	}

	printTable(results)
}

func loadList(path string) ([]Service, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []Service
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, rawURL, ok := parseLine(line)
		if !ok {
			fmt.Fprintf(os.Stderr, "aviso: línea %d ignorada: %q\n", lineNo, line)
			continue
		}
		if _, err := url.ParseRequestURI(rawURL); err != nil {
			fmt.Fprintf(os.Stderr, "aviso: URL inválida en línea %d (%s): %v\n", lineNo, name, err)
			continue
		}
		out = append(out, Service{Name: name, URL: rawURL})
	}
	return out, sc.Err()
}

func parseLine(line string) (name, rawURL string, ok bool) {
	if strings.Contains(line, "|") {
		parts := strings.SplitN(line, "|", 2)
		name = strings.TrimSpace(parts[0])
		rawURL = strings.TrimSpace(parts[1])
		if name == "" || rawURL == "" {
			return "", "", false
		}
		return name, rawURL, true
	}

	fields := strings.Fields(line)
	if len(fields) < 2 {
		// una sola columna: usar host como nombre
		if len(fields) == 1 && looksLikeURL(fields[0]) {
			u, err := url.Parse(fields[0])
			if err != nil {
				return "", "", false
			}
			n := u.Hostname()
			if n == "" {
				n = fields[0]
			}
			return n, fields[0], true
		}
		return "", "", false
	}
	rawURL = fields[len(fields)-1]
	name = strings.Join(fields[:len(fields)-1], " ")
	if !looksLikeURL(rawURL) {
		return "", "", false
	}
	return name, rawURL, true
}

func looksLikeURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func checkAll(services []Service, client *http.Client, timeout time.Duration, workers int, method string) []Result {
	if workers < 1 {
		workers = 1
	}
	jobs := make(chan Service)
	out := make(chan Result, len(services))

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for s := range jobs {
				out <- checkOne(s, client, timeout, method)
			}
		}()
	}

	go func() {
		for _, s := range services {
			jobs <- s
		}
		close(jobs)
		wg.Wait()
		close(out)
	}()

	results := make([]Result, 0, len(services))
	for r := range out {
		results = append(results, r)
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Status != results[j].Status {
			return statusRank(results[i].Status) < statusRank(results[j].Status)
		}
		return strings.ToLower(results[i].Name) < strings.ToLower(results[j].Name)
	})
	return results
}

func statusRank(s Status) int {
	switch s {
	case StatusOK:
		return 0
	case StatusBlocked:
		return 1
	case StatusDown:
		return 2
	default:
		return 3
	}
}

func checkOne(s Service, client *http.Client, timeout time.Duration, method string) Result {
	res := Result{Name: s.Name, URL: s.URL}
	start := time.Now()

	host := hostOf(s.URL)
	if host != "" {
		ips, err := net.LookupHost(host)
		if err != nil {
			res.Latency = time.Since(start)
			res.Status = StatusDown
			res.Reason = "DNS: " + shortErr(err)
			return res
		}
		if len(ips) > 0 {
			res.ResolvedIP = ips[0]
			if isSinkhole(ips) {
				res.Latency = time.Since(start)
				res.Status = StatusBlocked
				res.Reason = "DNS sinkhole (" + strings.Join(ips, ", ") + ")"
				return res
			}
		}
	}

	code, reason, err := doRequest(client, s.URL, method, timeout)
	res.Latency = time.Since(start)
	res.HTTPCode = code

	if err != nil {
		res.Status, res.Reason = classifyNetErr(err)
		return res
	}

	res.Reason = reason
	switch {
	case code == 451:
		res.Status = StatusBlocked
		if res.Reason == "" {
			res.Reason = "HTTP 451 Unavailable For Legal Reasons"
		}
	case code == 403:
		res.Status = StatusBlocked
		if res.Reason == "" {
			res.Reason = "HTTP 403 Forbidden"
		}
	case code >= 200 && code < 400:
		res.Status = StatusOK
		if res.Reason == "" {
			res.Reason = http.StatusText(code)
		}
	case code == 401:
		// el host responde: el servicio existe, pide auth
		res.Status = StatusOK
		res.Reason = "HTTP 401 Unauthorized (el servicio responde)"
	case code >= 500:
		res.Status = StatusDown
		if res.Reason == "" {
			res.Reason = http.StatusText(code)
		}
	default:
		res.Status = StatusError
		if res.Reason == "" {
			res.Reason = fmt.Sprintf("HTTP %d", code)
		}
	}
	return res
}

func doRequest(client *http.Client, rawURL, method string, timeout time.Duration) (int, string, error) {
	try := func(m string) (int, string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, m, rawURL, nil)
		if err != nil {
			return 0, "", err
		}
		req.Header.Set("User-Agent", "urlcheck/"+version+" (+local connectivity check)")
		req.Header.Set("Accept", "*/*")

		resp, err := client.Do(req)
		if err != nil {
			return 0, "", err
		}
		defer resp.Body.Close()
		_, _ = io.CopyN(io.Discard, resp.Body, 2048)
		return resp.StatusCode, strings.TrimSpace(resp.Status), nil
	}

	code, reason, err := try(method)
	if method == "HEAD" {
		if err != nil {
			return try("GET")
		}
		switch code {
		case http.StatusMethodNotAllowed, http.StatusNotImplemented, http.StatusExpectationFailed:
			return try("GET")
		}
	}
	return code, reason, err
}

func classifyNetErr(err error) (Status, string) {
	msg := shortErr(err)
	low := strings.ToLower(msg)

	switch {
	case strings.Contains(low, "timeout") || strings.Contains(low, "deadline exceeded"):
		return StatusDown, "timeout: " + msg
	case strings.Contains(low, "connection refused"):
		return StatusDown, "conexión rechazada"
	case strings.Contains(low, "no such host") || strings.Contains(low, "server misbehaving"):
		return StatusDown, "DNS: " + msg
	case strings.Contains(low, "network is unreachable"):
		return StatusDown, "red inalcanzable"
	case strings.Contains(low, "certificate") || strings.Contains(low, "tls"):
		return StatusError, "TLS: " + msg
	default:
		return StatusError, msg
	}
}

func shortErr(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if ue, ok := err.(*url.Error); ok && ue.Err != nil {
		s = ue.Err.Error()
	}
	if len(s) > 140 {
		s = s[:137] + "..."
	}
	return s
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func isSinkhole(ips []string) bool {
	for _, ip := range ips {
		if sinkholeIPs[ip] {
			return true
		}
		parsed := net.ParseIP(ip)
		if parsed != nil && parsed.IsLoopback() {
			return true
		}
	}
	return false
}

func printTable(results []Result) {
	nameW, urlW := 8, 8
	for _, r := range results {
		if len(r.Name) > nameW {
			nameW = len(r.Name)
		}
		if len(r.URL) > urlW {
			urlW = len(r.URL)
		}
	}
	if nameW > 28 {
		nameW = 28
	}
	if urlW > 48 {
		urlW = 48
	}

	useColor := isTTY()
	fmt.Printf("%s  %-*s  %-*s  %6s  %4s  %s\n",
		pad("ESTADO", 9), nameW, "SERVICIO", urlW, "URL", "ms", "HTTP", "DETALLE")
	fmt.Println(strings.Repeat("─", 9+2+nameW+2+urlW+2+6+2+4+2+24))

	var okN, blockedN, downN, errN int
	for _, r := range results {
		switch r.Status {
		case StatusOK:
			okN++
		case StatusBlocked:
			blockedN++
		case StatusDown:
			downN++
		default:
			errN++
		}

		st := string(r.Status)
		if useColor {
			st = colorStatus(r.Status)
		}
		httpS := "-"
		if r.HTTPCode > 0 {
			httpS = fmt.Sprintf("%d", r.HTTPCode)
		}
		fmt.Printf("%s  %-*s  %-*s  %6d  %4s  %s\n",
			pad(st, 9+colorPad(useColor, r.Status)),
			nameW, truncate(r.Name, nameW),
			urlW, truncate(r.URL, urlW),
			r.latencyMS(),
			httpS,
			r.Reason,
		)
	}

	fmt.Println()
	fmt.Printf("resumen: %d OK · %d BLOCKED · %d DOWN · %d ERROR  (%d total)\n",
		okN, blockedN, downN, errN, len(results))
	if blockedN == 0 && downN == 0 && errN == 0 {
		fmt.Println("todos los servicios de la lista responden desde esta red.")
	}
}

func pad(s string, w int) string {
	vis := visibleLen(s)
	if vis >= w {
		return s
	}
	return s + strings.Repeat(" ", w-vis)
}

func visibleLen(s string) int {
	n := 0
	in := false
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			in = true
			continue
		}
		if in {
			if s[i] == 'm' {
				in = false
			}
			continue
		}
		n++
	}
	return n
}

func colorPad(use bool, st Status) int {
	if !use {
		return 0
	}
	return len(colorStatus(st)) - len(string(st))
}

func colorStatus(st Status) string {
	switch st {
	case StatusOK:
		return "\033[32mOK\033[0m"
	case StatusBlocked:
		return "\033[33mBLOCKED\033[0m"
	case StatusDown:
		return "\033[31mDOWN\033[0m"
	default:
		return "\033[35mERROR\033[0m"
	}
}

func truncate(s string, w int) string {
	if len(s) <= w {
		return s
	}
	if w <= 1 {
		return s[:w]
	}
	return s[:w-1] + "…"
}

func isTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
