package approval

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	"github.com/EndoTheDev/omega/core"
)

// The safeagents detector set: deterministic, request-only, plain Go regexes - zero
// dependencies, zero network.
//
// Two questions only a request can answer:
//  1. PROVENANCE - does the host this call reaches appear in the HUMAN's own words? A host
//     the model wrote earlier is no more sourced than one it writes now, so assistant
//     turns are excluded: self-citation is not provenance.
//  2. REACH - a host only counts in a call that can reach it: the tool name, or a
// line carrying a network client. A URL named above its `curl` still counts.
//
// Severity decides routing: SeverityHigh findings block on their own, the milder tiers
// mark the call SUSPICIOUS (judge, then human). Every finding is a deterministic fact
// about the request - no model, no history, no conversation-dependent heuristics.

// Detector names - stable and greppable: they ride the audit row, the judge payload and
// the human card. The string is the contract.
const (
	DetectorHostProvenance  = "host-provenance"
	DetectorProcEnviron     = "proc-environ"
	DetectorProxyBypass     = "proxy-bypass"
	DetectorK8sToken        = "k8s-token"
	DetectorNamespaceEscape = "namespace-escape"
	DetectorNetrcRead       = "netrc-read"
	DetectorHarnessSecret   = "harness-secret"
	// the judge's own additions (half 2) - never deterministic findings
	DetectorJudgeEvidence = "judge-evidence"
	DetectorJudgeFailure  = "judge-failure"
)

// maxTaskChars caps the human task text the judge reads.
const maxTaskChars = 2000

// Provenance is the conversation's human-authored text - the only citation that sources a
// host. Assistant turns are excluded by rule, so a host the model invented earlier reads
// exactly like one it invents now.
type Provenance struct {
	turns []string
}

// NewProvenance extracts the human-authored turns from one run's conversation. A nil
// conversation (one-shot CLI paths, tests) is the documented floor: nothing is sourced, so
// every non-allowlisted host reads as novel.
func NewProvenance(conversation []core.Message) Provenance {
	p := Provenance{}
	for _, m := range conversation {
		switch msg := m.(type) {
		case core.User:
			p.turns = append(p.turns, msg.Content)
		case *core.User:
			if msg != nil {
				p.turns = append(p.turns, msg.Content)
			}
		}
	}
	return p
}

// Sources reports whether the human's own words ever named host. The match is
// label-bound: "evil.com" is NOT sourced by "notevil.com", while a subdomain citation
// sources its registrable name (api.evil.com cites evil.com).
func (p Provenance) Sources(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "" {
		return false
	}
	for _, turn := range p.turns {
		text := strings.ToLower(turn)
		for from := 0; ; {
			i := strings.Index(text[from:], h)
			if i < 0 {
				break
			}
			i += from
			from = i + len(h)
			if labelBoundary(text, i, i+len(h)) {
				return true
			}
		}
	}
	return false
}

// labelBoundary reports whether s[lo:hi] stands alone as a host name:
// the neighbours are not host-label characters.
func labelBoundary(s string, lo, hi int) bool {
	hostChar := func(b byte) bool {
		switch {
		case b >= 'a' && b <= 'z', b >= '0' && b <= '9', b == '-':
			return true
		}
		return false
	}
	if lo > 0 && hostChar(s[lo-1]) {
		return false
	}
	if hi < len(s) && hostChar(s[hi]) {
		return false
	}
	return true
}

// Task returns the run's own request - the LAST human turn - for the evidence-only judge.
// Empty = this run carried no human turn.
func (p Provenance) Task() string {
	if len(p.turns) == 0 {
		return ""
	}
	return truncTo(strings.TrimSpace(p.turns[len(p.turns)-1]), maxTaskChars)
}

// Call is one pending tool call as the detectors see it: the tool name and its DECODED
// argument tree. Nothing else - the agent's reasoning is not a detector input here.
type Call struct {
	Tool string
	Args map[string]any
}

// Text flattens the call into the text the detectors read: one line per decoded argument
// line. A JSON payload carried as a string is decoded first - matching raw wire text would
// let an escaped "\n" in front of "cat" read as "ncat" (rule: decode, then match per line).
func (c Call) Text() string {
	return strings.Join(c.lines(), "\n")
}

// lines is the decoded argument tree as text lines, keys walked in sorted order.
func (c Call) lines() []string {
	var out []string
	walkArgValue(c.Args, &out)
	return out
}

func walkArgValue(v any, out *[]string) {
	switch t := v.(type) {
	case string:
		// a string that is itself a JSON document (some tools take
		// their payload as one) is decoded once and walked
		if len(t) > 1 && (t[0] == '{' || t[0] == '[') {
			var inner any
			if err := json.Unmarshal([]byte(t), &inner); err == nil {
				walkArgValue(inner, out)
				return
			}
		}
		for _, line := range strings.Split(t, "\n") {
			*out = append(*out, line)
		}
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			walkArgValue(t[k], out)
		}
	case []any:
		for _, e := range t {
			walkArgValue(e, out)
		}
	}
}

var (
	// urlHostPattern reads the authority out of a URL:
	// scheme://[userinfo@]host[:port][/...].
	urlHostPattern = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.\-]*://(?:[^\s/@]+@)?(\[[0-9a-f:.]+\]|[a-z0-9._\-]+)`)
	// ipv4Pattern is a dotted quad; octet validation and the "="
	// exclusion happen in Go (a version tail is not an address).
	ipv4Pattern = regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}\b`)
	// reachToolPattern: a tool NAME that can reach a network.
	reachToolPattern = regexp.MustCompile(`(?i)fetch|browse|browser|web|http|url|download`)
	// reachLinePattern: command lines that open a connection - the
	// rule's set (curl, wget, nc, ssh, git clone, pip install,
	// /dev/tcp/, requests., httpx.) plus the clients the legacy
	// detector already knows.
	reachLinePattern = regexp.MustCompile(`(?i)(\bcurl\b|\bwget\b|\bnc\b|\bncat\b|\bnetcat\b|\btelnet\b|\bssh\b|\bscp\b|\bsftp\b|\bgit\s+clone\b|\bpip3?\s+install\b|\bnpm\s+(?:i|install)\b|/dev/tcp/|\brequests\.|\bhttpx\b|\bhttp\.client\b|\burllib\b|\baiohttp\b)`)

	// the Cohere-report behaviours: what an agent does when it decides
	// the conversation is not watching.
	procEnvironPattern     = regexp.MustCompile(`(?i)/proc/(?:self|\*|\d+)/environ\b`)
	proxyBypassPattern     = regexp.MustCompile(`(?i)--no-?proxy\b`)
	k8sTokenPattern        = regexp.MustCompile(`(?i)/var/run/secrets/kubernetes\.io/serviceaccount|serviceaccount/token|kubelet\.conf`)
	namespaceEscapePattern = regexp.MustCompile(`(?i)(?:^|[\s;&|(])(?:unshare|nsenter)\b`)
	netrcPattern           = regexp.MustCompile(`(?i)(?:^|[\s=@'"/~])[^\s'"]*\.netrc\b`)
	harnessSecretPattern   = regexp.MustCompile(`(?i)(?:^|[\s'"])[^\s'"]*harness[/\\][^\s'"]*secret[^\s'"]*|(?:^|[\s'"])[^\s'"]*\.omega[/\\]config[/\\](?:config|trust)\.yaml`)
	readVerbPattern        = regexp.MustCompile(`(?i)(?:^|[\s;&|(])(?:cat|less|more|head|tail|strings|xxd|hexdump|od|base64|grep|rg|awk|sed|cp|scp|tar|zip|python3?|node|perl|ruby|php|curl|wget|nc)\b`)
)

// allowlistDomains: the package/tooling hosts the rule allows verbatim.
var allowlistDomains = []string{
	"github.com", "pypi.org", "pythonhosted.org", "huggingface.co",
	"hf.co", "npmjs.org", "crates.io",
}

// reservedDomains: RFC 2606 / RFC 6761 special-use names - never registrable, so naming
// one can never reach a third party.
var reservedDomains = []string{
	"example.com", "example.net", "example.org",
	"example", "test", "invalid", "localhost",
}

// allowlistedHost reports whether a host is exempt from the provenance question. The
// loopback spellings (127.0.0.1, ::1, 0.0.0.0) are the localhost rule, not a new entry.
func allowlistedHost(host string) bool {
	h := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	switch h {
	case "localhost", "127.0.0.1", "::1", "0.0.0.0":
		return true
	}
	for _, d := range allowlistDomains {
		if h == d || strings.HasSuffix(h, "."+d) {
			return true
		}
	}
	for _, d := range reservedDomains {
		if h == d || strings.HasSuffix(h, "."+d) {
			return true
		}
	}
	return false
}

// DetectCall runs the safeagents detector set over one call. prov is
// the conversation's human-authored text (NewProvenance).
func DetectCall(call Call, prov Provenance) []Finding {
	if strings.TrimSpace(call.Tool) == "" {
		return nil
	}
	lines := call.lines()
	toolReaches := reachToolPattern.MatchString(call.Tool)
	callReaches := toolReaches
	for _, line := range lines {
		if reachLinePattern.MatchString(line) {
			callReaches = true
		}
	}

	var out []Finding
	seen := make(map[string]bool, 4)
	add := func(f Finding) {
		key := f.Detector + "|" + f.Detail
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, f)
	}
	high := func(detector, detail string) Finding {
		return Finding{Detector: detector, Severity: SeverityHigh, Tiered: true, Detail: detail}
	}
	suspicious := func(detector, detail string) Finding {
		return Finding{Detector: detector, Severity: SeverityMedium, Tiered: true, Detail: detail}
	}

	// HOST-PROVENANCE. A host only counts in a call that can reach it: a URL host counts
	// anywhere in a reaching call (the code names the URL a line before opening it), a
	// bare IP only on a reaching line - or on any line when the tool NAME is the reach
	// surface (`web.fetch`'s arguments are what it fetches). One finding per host.
	if callReaches {
		novel := map[string]string{}
		for _, line := range lines {
			for _, host := range urlHosts(line) {
				if allowlistedHost(host) || prov.Sources(host) {
					continue
				}
				if _, ok := novel[host]; !ok {
					novel[host] = "host " + host + " appears nowhere in the human's own words"
				}
			}
			if !toolReaches && !reachLinePattern.MatchString(line) {
				continue
			}
			for _, ip := range ipv4Hosts(line) {
				if allowlistedHost(ip) || prov.Sources(ip) {
					continue
				}
				if _, ok := novel[ip]; !ok {
					novel[ip] = "bare IP " + ip + " appears nowhere in the human's own words"
				}
			}
		}
		hosts := make([]string, 0, len(novel))
		for host := range novel {
			hosts = append(hosts, host)
		}
		sort.Strings(hosts)
		for _, host := range hosts {
			add(high(DetectorHostProvenance, novel[host]))
		}
	}

	// the behaviour set: what the call DOES, reachable or not.
	for _, line := range lines {
		if m := procEnvironPattern.FindString(line); m != "" {
			add(high(DetectorProcEnviron, m+" - another process's environment"))
		}
		if m := proxyBypassPattern.FindString(line); m != "" {
			add(high(DetectorProxyBypass, m+" disables the proxy the machine's egress policy rides"))
		}
		if m := k8sTokenPattern.FindString(line); m != "" {
			add(high(DetectorK8sToken, m+" - the orchestrator's service-account credential"))
		}
		if m := namespaceEscapePattern.FindString(line); m != "" {
			add(suspicious(DetectorNamespaceEscape, strings.TrimSpace(m)+" enters a namespace the call did not start in"))
		}
		if m := netrcPattern.FindString(line); m != "" && readVerbPattern.MatchString(line) {
			add(suspicious(DetectorNetrcRead, "reads "+strings.TrimSpace(m)+" - stored credentials for other hosts"))
		}
		if m := harnessSecretPattern.FindString(line); m != "" && readVerbPattern.MatchString(line) {
			add(high(DetectorHarnessSecret, "reads "+strings.TrimSpace(m)+" - the agent harness's own credential surface"))
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Severity != out[j].Severity {
			return severityRank(out[i].Severity) > severityRank(out[j].Severity)
		}
		if out[i].Detector != out[j].Detector {
			return out[i].Detector < out[j].Detector
		}
		return out[i].Detail < out[j].Detail
	})
	return out
}

// urlHosts extracts the host of every URL on one line.
func urlHosts(line string) []string {
	var out []string
	for _, m := range urlHostPattern.FindAllStringSubmatch(line, -1) {
		h := strings.ToLower(strings.Trim(m[1], "[]"))
		h = strings.TrimSuffix(h, ".")
		if h != "" {
			out = append(out, h)
		}
	}
	return out
}

// ipv4Hosts returns the dotted quads on one line that are really addresses: digits after
// "=" are a version, not an IP (`pkg==0.5.1.4`), and every octet must fit a byte.
func ipv4Hosts(line string) []string {
	var out []string
	for _, loc := range ipv4Pattern.FindAllStringIndex(line, -1) {
		if loc[0] > 0 && line[loc[0]-1] == '=' {
			continue
		}
		if loc[1] < len(line) && line[loc[1]] == '.' {
			continue // 1.2.3.4.5: a version tail, not a quad
		}
		cand := line[loc[0]:loc[1]]
		if validIPv4(cand) {
			out = append(out, cand)
		}
	}
	return out
}

// validIPv4: four dot-separated octets, each 0..255.
func validIPv4(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		return false
	}
	for _, p := range parts {
		if p == "" || len(p) > 3 {
			return false
		}
		n := 0
		for _, r := range p {
			if r < '0' || r > '9' {
				return false
			}
			n = n*10 + int(r-'0')
		}
		if n > 255 {
			return false
		}
	}
	return true
}
